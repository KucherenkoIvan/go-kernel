package grpckit

import (
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Connect creates a client connection with the standard interceptor chain.
// Wrap it with a generated client — that pair is the "universal client":
// serialization, request-id propagation, and error decoding are already
// handled for unary and streaming calls, adapters only map types.
//
//	conn := grpckit.MustConnect(cfg.CouponsGRPCURL)
//	client := couponsv1.NewCouponServiceClient(conn)
//
// target is any gRPC name: "host:port", "dns:///host:port", or
// "unix:///abs/path" for a server on a Unix socket (see Serve).
//
// Transport is plaintext (service-to-service inside a private network); TLS
// can be added as an option when a deployment needs it.
func Connect(target string, extra ...grpc.DialOption) (*grpc.ClientConn, error) {
	opts := append([]grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(clientInterceptor()),
		grpc.WithChainStreamInterceptor(streamClientInterceptor()),
	}, extra...)

	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, fmt.Errorf("grpckit: connect %s: %w", target, err)
	}
	return conn, nil
}

// MustConnect is Connect that panics on error — for use in main().
func MustConnect(target string, extra ...grpc.DialOption) *grpc.ClientConn {
	conn, err := Connect(target, extra...)
	if err != nil {
		panic(err)
	}
	return conn
}
