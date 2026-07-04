package grpckit_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/KucherenkoIvan/go-kernel/auth"
	examplev1 "github.com/KucherenkoIvan/go-kernel/contracts/gen/grpc/example/v1"
	"github.com/KucherenkoIvan/go-kernel/ddd"
	"github.com/KucherenkoIvan/go-kernel/grpckit"
	"github.com/KucherenkoIvan/go-kernel/grpckit/grpckittest"
)

type bannedError struct{ ddd.DomainError }

func (e *bannedError) Error() string { return "user_banned" }

// The auth pattern from the middleware guide: a custom interceptor verifies
// metadata and puts a Principal into the context.
func authInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		switch token := md.Get("authorization"); {
		case len(token) == 0:
			return nil, status.Error(codes.Unauthenticated, "unauthenticated")
		case token[0] == "banned":
			return nil, &bannedError{} // domain errors from interceptors are encoded too
		case token[0] == "panic":
			panic("auth exploded") // recovered by the standard chain
		}
		return handler(auth.WithPrincipal(ctx, auth.Principal{UserID: "u-1"}), req)
	}
}

type principalEchoServer struct {
	examplev1.UnimplementedEchoServiceServer
}

func (s *principalEchoServer) Echo(ctx context.Context, _ *examplev1.EchoRequest) (*examplev1.EchoResponse, error) {
	p, ok := auth.PrincipalFrom(ctx)
	if !ok {
		return nil, errors.New("principal must be set by the auth interceptor")
	}
	return &examplev1.EchoResponse{Message: p.UserID}, nil
}

func setupWithAuth(t *testing.T) examplev1.EchoServiceClient {
	t.Helper()

	srv := grpckit.NewServer(
		grpckit.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		grpckit.WithUnaryInterceptor(authInterceptor()),
	)
	examplev1.RegisterEchoServiceServer(srv, &principalEchoServer{})
	return examplev1.NewEchoServiceClient(grpckittest.Serve(t, srv))
}

func call(client examplev1.EchoServiceClient, token string) (*examplev1.EchoResponse, error) {
	ctx := context.Background()
	if token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", token)
	}
	return client.Echo(ctx, &examplev1.EchoRequest{Message: "hi"})
}

func TestCustomInterceptor_AuthFlow(t *testing.T) {
	client := setupWithAuth(t)

	if _, err := call(client, ""); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("no token: %v, want Unauthenticated", err)
	}

	resp, err := call(client, "valid")
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetMessage() != "u-1" {
		t.Fatalf("handler did not see the principal: %q", resp.GetMessage())
	}
}

func TestCustomInterceptor_DomainErrorEncoded(t *testing.T) {
	client := setupWithAuth(t)

	_, err := call(client, "banned")
	var remote *grpckit.RemoteDomainError
	if !errors.As(err, &remote) || remote.Code != "user_banned" {
		t.Fatalf("domain error from interceptor not encoded: %v", err)
	}
}

func TestCustomInterceptor_PanicRecovered(t *testing.T) {
	client := setupWithAuth(t)

	_, err := call(client, "panic")
	if status.Code(err) != codes.Internal {
		t.Fatalf("panic in custom interceptor: %v, want Internal", err)
	}
}
