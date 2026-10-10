package grpckit_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	examplev1 "github.com/KucherenkoIvan/go-kernel/contracts/gen/grpc/example/v1"
	"github.com/KucherenkoIvan/go-kernel/grpckit"
)

func TestServe_UnixSocket(t *testing.T) {
	// Not t.TempDir(): its path embeds the test name and can exceed the
	// ~104-byte sun_path limit on macOS.
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

	srv := grpckit.NewServer(grpckit.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	impl := &echoServer{}
	examplev1.RegisterEchoServiceServer(srv, impl)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- grpckit.Serve(ctx, srv, lis) }()

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
	select {
	case err := <-served:
		if err != nil {
			t.Errorf("Serve after cancel = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after ctx cancel")
	}
}

// failingListener's Accept fails permanently, as a broken listener would.
type failingListener struct{ net.Listener }

func (failingListener) Accept() (net.Conn, error) { return nil, errors.New("accept: broken") }
func (failingListener) Close() error              { return nil }
func (failingListener) Addr() net.Addr            { return &net.UnixAddr{Name: "broken", Net: "unix"} }

func TestServe_ListenerFailureReturnsWithoutCancel(t *testing.T) {
	srv := grpckit.NewServer(grpckit.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))

	served := make(chan error, 1)
	go func() { served <- grpckit.Serve(context.Background(), srv, failingListener{}) }()

	select {
	case err := <-served:
		if err == nil {
			t.Error("Serve = nil, want the accept error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve hung on a failed listener instead of returning its error")
	}
}
