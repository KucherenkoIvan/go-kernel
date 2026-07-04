package grpckit_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	examplev1 "github.com/KucherenkoIvan/go-kernel/contracts/gen/grpc/example/v1"
	"github.com/KucherenkoIvan/go-kernel/ddd"
	"github.com/KucherenkoIvan/go-kernel/grpckit"
	"github.com/KucherenkoIvan/go-kernel/grpckit/grpckittest"
)

type echoForbiddenError struct{ ddd.DomainError }

func (e *echoForbiddenError) Error() string { return "echo_forbidden" }

type echoServer struct {
	examplev1.UnimplementedEchoServiceServer
	gotRequestID string
}

func (s *echoServer) Echo(ctx context.Context, req *examplev1.EchoRequest) (*examplev1.EchoResponse, error) {
	s.gotRequestID = grpckit.RequestID(ctx)
	switch req.GetMessage() {
	case "forbidden":
		return nil, &echoForbiddenError{}
	case "panic":
		panic("boom")
	case "db":
		return nil, errors.New("connection refused to 10.0.0.5:5432")
	}
	return &examplev1.EchoResponse{Message: req.GetMessage()}, nil
}

func setup(t *testing.T) (examplev1.EchoServiceClient, *echoServer) {
	t.Helper()

	impl := &echoServer{}
	srv := grpckit.NewServer(
		grpckit.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		grpckit.WithErrorCode("echo_forbidden", codes.PermissionDenied),
	)
	examplev1.RegisterEchoServiceServer(srv, impl)

	conn := grpckittest.Serve(t, srv)
	return examplev1.NewEchoServiceClient(conn), impl
}

func TestEcho_HappyPath_PropagatesRequestID(t *testing.T) {
	client, impl := setup(t)

	ctx := grpckit.WithRequestID(context.Background(), "req-42")
	resp, err := client.Echo(ctx, &examplev1.EchoRequest{Message: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetMessage() != "hello" {
		t.Errorf("Message = %q", resp.GetMessage())
	}
	if impl.gotRequestID != "req-42" {
		t.Errorf("request id on server = %q, want req-42", impl.gotRequestID)
	}
}

func TestEcho_GeneratesRequestID(t *testing.T) {
	client, impl := setup(t)

	if _, err := client.Echo(context.Background(), &examplev1.EchoRequest{Message: "hello"}); err != nil {
		t.Fatal(err)
	}
	if impl.gotRequestID == "" {
		t.Error("server should always see a request id")
	}
}

func TestEcho_DomainErrorCrossesTheWire(t *testing.T) {
	client, _ := setup(t)

	_, err := client.Echo(context.Background(), &examplev1.EchoRequest{Message: "forbidden"})
	if err == nil {
		t.Fatal("expected error")
	}

	if !ddd.IsDomainError(err) {
		t.Fatalf("domain error lost in transport: %v", err)
	}
	var remote *grpckit.RemoteDomainError
	if !errors.As(err, &remote) || remote.Code != "echo_forbidden" {
		t.Fatalf("RemoteDomainError not restored: %v", err)
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("status = %v, want PermissionDenied (WithErrorCode mapping)", status.Code(err))
	}
}

func TestEcho_PanicBecomesInternal(t *testing.T) {
	client, _ := setup(t)

	_, err := client.Echo(context.Background(), &examplev1.EchoRequest{Message: "panic"})
	if status.Code(err) != codes.Internal {
		t.Fatalf("status = %v, want Internal", status.Code(err))
	}
	if ddd.IsDomainError(err) {
		t.Error("panic must not look like a domain error")
	}
}

func TestEcho_InternalErrorIsMasked(t *testing.T) {
	client, _ := setup(t)

	_, err := client.Echo(context.Background(), &examplev1.EchoRequest{Message: "db"})
	if status.Code(err) != codes.Internal {
		t.Fatalf("status = %v, want Internal", status.Code(err))
	}
	if st, _ := status.FromError(err); st.Message() != "internal error" {
		t.Errorf("internal details leaked to the client: %q", st.Message())
	}
}
