// Package grpckit builds gRPC servers and clients with the kernel's standard
// interceptor chain, so services never hand-write the serialize/send or
// deserialize/route path:
//
//   - server: panic recovery, request-id, logging, ddd domain errors encoded
//     into rich statuses (never leaking internal errors);
//   - client: request-id propagation, rich statuses decoded back into domain
//     errors (RemoteDomainError).
package grpckit

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

type serverConfig struct {
	logger     *slog.Logger
	errorCodes map[string]codes.Code
}

// ServerOption customizes NewServer.
type ServerOption func(*serverConfig)

// WithLogger sets the logger for handler errors and panics.
// Defaults to slog.Default().
func WithLogger(logger *slog.Logger) ServerOption {
	return func(c *serverConfig) { c.logger = logger }
}

// WithErrorCode maps a domain error code to a gRPC status code, e.g.
//
//	grpckit.WithErrorCode("coupon_not_found", codes.NotFound)
//
// Unmapped domain errors default to codes.FailedPrecondition.
func WithErrorCode(domainCode string, grpcCode codes.Code) ServerOption {
	return func(c *serverConfig) { c.errorCodes[domainCode] = grpcCode }
}

// NewServer creates a *grpc.Server with the standard interceptor chain.
// Register generated services on it as usual, then start it with Run.
func NewServer(opts ...ServerOption) *grpc.Server {
	cfg := &serverConfig{
		logger:     slog.Default(),
		errorCodes: map[string]codes.Code{},
	}
	for _, opt := range opts {
		opt(cfg)
	}

	codeFor := func(code string) codes.Code {
		if c, ok := cfg.errorCodes[code]; ok {
			return c
		}
		return codes.FailedPrecondition
	}

	return grpc.NewServer(
		grpc.ChainUnaryInterceptor(serverInterceptor(cfg.logger, codeFor)),
	)
}

// Run listens on addr and serves until ctx is cancelled, then stops
// gracefully. Blocks; returns the Serve error, nil on graceful stop.
func Run(ctx context.Context, srv *grpc.Server, addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("grpckit: listen %s: %w", addr, err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		srv.GracefulStop()
	}()

	err = srv.Serve(lis)
	<-done
	if err == grpc.ErrServerStopped || ctx.Err() != nil {
		return nil
	}
	return err
}
