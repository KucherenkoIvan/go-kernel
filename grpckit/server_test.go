package grpckit_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"

	examplev1 "github.com/KucherenkoIvan/go-kernel/contracts/gen/grpc/example/v1"
	"github.com/KucherenkoIvan/go-kernel/grpckit"
)

func quietServer() *grpc.Server {
	return grpckit.NewServer(grpckit.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
}

// unixListener opens a Unix socket in a fresh directory. Not t.TempDir(): its
// path embeds the test name and can exceed the ~104-byte sun_path limit on
// macOS.
func unixListener(t *testing.T) (net.Listener, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "grpckit")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "api.sock")

	lis, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	return lis, sock
}

// startServe runs grpckit.Serve in the background; the returned channel
// yields its result.
func startServe(ctx context.Context, srv *grpc.Server, lis net.Listener) <-chan error {
	served := make(chan error, 1)
	go func() { served <- grpckit.Serve(ctx, srv, lis) }()
	return served
}

func waitServe(t *testing.T, served <-chan error, within time.Duration) error {
	t.Helper()
	select {
	case err := <-served:
		return err
	case <-time.After(within):
		t.Fatalf("Serve did not return within %v", within)
		return nil
	}
}

func TestServe_UnixSocket(t *testing.T) {
	lis, sock := unixListener(t)
	srv := quietServer()
	impl := &echoServer{}
	examplev1.RegisterEchoServiceServer(srv, impl)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := startServe(ctx, srv, lis)

	conn, err := grpckit.Connect("unix://" + sock)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	callCtx := grpckit.WithRequestID(context.Background(), "req-unix")
	resp, err := examplev1.NewEchoServiceClient(conn).Echo(callCtx, &examplev1.EchoRequest{Message: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetMessage() != "hello" {
		t.Errorf("Message = %q", resp.GetMessage())
	}
	if impl.gotRequestID != "req-unix" {
		t.Errorf("request id on server = %q, want req-unix", impl.gotRequestID)
	}

	cancel()
	if err := waitServe(t, served, 5*time.Second); err != nil {
		t.Errorf("Serve after cancel = %v, want nil", err)
	}
	if _, err := os.Stat(sock); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("socket file still there after Serve returned: %v", err)
	}
}

// blockingEchoServer holds every call until the server closes its connection.
type blockingEchoServer struct {
	examplev1.UnimplementedEchoServiceServer
	entered chan struct{}
}

func (s *blockingEchoServer) Echo(ctx context.Context, _ *examplev1.EchoRequest) (*examplev1.EchoResponse, error) {
	close(s.entered)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestServe_ShutdownIsBounded(t *testing.T) {
	const timeout = 300 * time.Millisecond
	grpckit.SetShutdownTimeout(t, timeout)

	lis, sock := unixListener(t)
	srv := quietServer()
	impl := &blockingEchoServer{entered: make(chan struct{})}
	examplev1.RegisterEchoServiceServer(srv, impl)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := startServe(ctx, srv, lis)

	conn, err := grpckit.Connect("unix://" + sock)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	go func() {
		_, _ = examplev1.NewEchoServiceClient(conn).Echo(context.Background(), &examplev1.EchoRequest{Message: "hold"})
	}()
	<-impl.entered

	start := time.Now()
	cancel()
	if err := waitServe(t, served, timeout+5*time.Second); err != nil {
		t.Errorf("Serve after cancel = %v, want nil", err)
	}
	if elapsed := time.Since(start); elapsed < timeout {
		t.Errorf("Serve returned after %v, before the %v grace period for the in-flight call", elapsed, timeout)
	}
}

// closeRecorder records whether Serve closed the listener it was handed.
type closeRecorder struct {
	net.Listener
	closed atomic.Bool
}

func (l *closeRecorder) Close() error {
	l.closed.Store(true)
	if l.Listener == nil {
		return nil
	}
	return l.Listener.Close()
}

// failingListener's Accept fails permanently, as a broken listener would.
type failingListener struct{ closeRecorder }

func (*failingListener) Accept() (net.Conn, error) { return nil, errors.New("accept: broken") }
func (*failingListener) Addr() net.Addr            { return &net.UnixAddr{Name: "broken.sock", Net: "unix"} }

func TestServe_ListenerFailureReturnsWithoutCancel(t *testing.T) {
	lis := &failingListener{}
	err := waitServe(t, startServe(context.Background(), quietServer(), lis), 5*time.Second)

	if err == nil || !strings.Contains(err.Error(), "grpckit: serve broken.sock: accept: broken") {
		t.Errorf("Serve = %v, want the wrapped accept error", err)
	}
	if !lis.closed.Load() {
		t.Error("Serve did not close the listener")
	}
}

func TestServe_PreCancelledContext(t *testing.T) {
	inner, _ := unixListener(t)
	lis := &closeRecorder{Listener: inner}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := waitServe(t, startServe(ctx, quietServer(), lis), 5*time.Second); err != nil {
		t.Errorf("Serve with a cancelled ctx = %v, want nil", err)
	}
	if !lis.closed.Load() {
		t.Error("Serve did not close the listener")
	}
}

func TestServe_ExternalStopIsAnError(t *testing.T) {
	lis, _ := unixListener(t)
	srv := quietServer()
	served := startServe(context.Background(), srv, lis)

	srv.Stop()
	if err := waitServe(t, served, 5*time.Second); !errors.Is(err, grpc.ErrServerStopped) {
		t.Errorf("Serve after an external Stop = %v, want ErrServerStopped while ctx is live", err)
	}
}
