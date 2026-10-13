//go:build unix

package app_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/KucherenkoIvan/go-kernel/app"
)

// A real second signal would kill the test binary, so these tests re-execute
// it as a subprocess running TestSignalHelper in the mode named by
// helperModeEnv, and assert on how that process ends. The helper prints
// "running" from inside a Run and "closing" from inside a Close, so the
// parent knows which phase it is signalling.
const helperModeEnv = "APP_SIGNAL_TEST_HELPER"

func TestSignalHelper(t *testing.T) {
	mode := os.Getenv(helperModeEnv)
	if mode == "" {
		t.Skip("subprocess helper for the signal tests")
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	running := func(ctx context.Context) error {
		fmt.Println("running")
		<-ctx.Done()
		return nil
	}
	hungClose := func(context.Context) error {
		fmt.Println("closing")
		time.Sleep(time.Hour) // ignores its ctx: only a forced quit ends this
		return nil
	}

	var err error
	switch mode {
	case "hung-close":
		err = app.RunWith(context.Background(), app.Options{Logger: logger},
			app.Adapter{Name: "stuck", Close: hungClose},
			app.Adapter{Name: "worker", Run: running},
		)
	case "failure-hung-close":
		err = app.RunWith(context.Background(), app.Options{Logger: logger},
			app.Adapter{Name: "stuck", Close: hungClose},
			app.Adapter{Name: "failing", Run: func(context.Context) error {
				fmt.Println("running")
				return errors.New("boom")
			}},
		)
	case "graceful":
		err = app.RunWith(context.Background(), app.Options{Logger: logger},
			app.Adapter{Name: "worker", Run: running, Close: func(context.Context) error {
				fmt.Println("closing")
				return nil
			}},
		)
	default:
		t.Fatalf("unknown helper mode %q", mode)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "run error:", err)
		os.Exit(3)
	}
	os.Exit(0)
}

type helper struct {
	cmd    *exec.Cmd
	lines  chan string
	stderr *syncBuffer
	exited chan error
}

func startHelper(t *testing.T, mode string) *helper {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSignalHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(), helperModeEnv+"="+mode)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	h := &helper{cmd: cmd, lines: make(chan string, 16), stderr: &syncBuffer{}, exited: make(chan error, 1)}
	cmd.Stderr = h.stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			h.lines <- sc.Text()
		}
		_, _ = io.Copy(io.Discard, stdout)
		h.exited <- cmd.Wait()
	}()
	return h
}

func (h *helper) waitFor(t *testing.T, line string) {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case got := <-h.lines:
			if got == line {
				return
			}
		case <-timeout:
			t.Fatalf("helper never printed %q; stderr:\n%s", line, h.stderr.String())
		}
	}
}

func (h *helper) waitForStderr(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(h.stderr.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("helper stderr never contained %q; stderr:\n%s", want, h.stderr.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (h *helper) signal(t *testing.T, sig os.Signal) {
	t.Helper()
	if err := h.cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
}

func (h *helper) wait(t *testing.T) error {
	t.Helper()
	select {
	case err := <-h.exited:
		return err
	case <-time.After(10 * time.Second):
		t.Fatalf("helper still running; stderr:\n%s", h.stderr.String())
		return nil
	}
}

func assertKilledBy(t *testing.T, err error, sig syscall.Signal, stderr string) {
	t.Helper()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("want death by %v, got %v; stderr:\n%s", sig, err, stderr)
	}
	ws, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() || ws.Signal() != sig {
		t.Fatalf("want death by %v, got %v; stderr:\n%s", sig, exitErr, stderr)
	}
}

func skipIfIgnored(t *testing.T, sig syscall.Signal) {
	t.Helper()
	// a process started with the signal ignored (a background job of a
	// non-interactive shell) passes that on to the helper; releasing the
	// handler then restores "ignore", not death
	if signal.Ignored(sig) {
		t.Skipf("%v is ignored in this environment", sig)
	}
}

func TestRun_SecondSignalKillsHungClose(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			skipIfIgnored(t, sig)
			h := startHelper(t, "hung-close")
			h.waitFor(t, "running")

			h.signal(t, sig)
			h.waitFor(t, "closing") // first signal: graceful shutdown reached Close
			// stderr reaches the parent through a copy goroutine of its own, so
			// it can lag the stdout marker above
			h.waitForStderr(t, "signal again to force quit")

			h.signal(t, sig)
			assertKilledBy(t, h.wait(t), sig, h.stderr.String())
		})
	}
}

func TestRun_FailureShutdownReleasesSignals(t *testing.T) {
	skipIfIgnored(t, syscall.SIGINT)
	h := startHelper(t, "failure-hung-close")
	h.waitFor(t, "closing") // shutdown began from the adapter failure

	h.signal(t, syscall.SIGINT) // the first signal is already a forced quit
	assertKilledBy(t, h.wait(t), syscall.SIGINT, h.stderr.String())
}

func TestRun_FirstSignalIsGraceful(t *testing.T) {
	skipIfIgnored(t, syscall.SIGINT)
	h := startHelper(t, "graceful")
	h.waitFor(t, "running")

	h.signal(t, syscall.SIGINT)
	h.waitFor(t, "closing")
	if err := h.wait(t); err != nil {
		t.Fatalf("one signal must end in a clean exit, got %v; stderr:\n%s", err, h.stderr.String())
	}
	for _, want := range []string{"signal again to force quit", "signal=interrupt", "adapter stopped", "adapter closed"} {
		if !strings.Contains(h.stderr.String(), want) {
			t.Errorf("stderr misses %q:\n%s", want, h.stderr.String())
		}
	}
}

type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
