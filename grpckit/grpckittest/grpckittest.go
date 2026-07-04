// Package grpckittest helps test gRPC services in-process, without a network.
package grpckittest

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/KucherenkoIvan/go-kernel/grpckit"
)

// Serve starts srv on an in-memory listener and returns a client connection
// created through grpckit.Connect — the standard client chain (request-id,
// error decoding) included, so tests exercise exactly what production runs.
// Server and connection are cleaned up automatically.
//
//	srv := grpckit.NewServer(grpckit.WithLogger(discard))
//	couponsv1.RegisterCouponServiceServer(srv, controller)
//	client := couponsv1.NewCouponServiceClient(grpckittest.Serve(t, srv))
func Serve(tb testing.TB, srv *grpc.Server, extra ...grpc.DialOption) *grpc.ClientConn {
	tb.Helper()

	lis := bufconn.Listen(1 << 20)
	go srv.Serve(lis) //nolint:errcheck
	tb.Cleanup(srv.Stop)

	opts := append([]grpc.DialOption{
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
	}, extra...)

	conn, err := grpckit.Connect("passthrough:///bufnet", opts...)
	if err != nil {
		tb.Fatalf("grpckittest: connect: %v", err)
	}
	tb.Cleanup(func() { _ = conn.Close() })
	return conn
}
