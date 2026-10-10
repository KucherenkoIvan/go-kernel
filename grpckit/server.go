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
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

type serverConfig struct {
	logger       *slog.Logger
	errorCodes   map[string]codes.Code
	interceptors []grpc.UnaryServerInterceptor
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

// WithUnaryInterceptor appends a custom interceptor (auth, rate limiting,
// metrics, ...) after the standard chain. May be given multiple times;
// interceptors run in the order they were added.
//
// The standard chain stays outermost, so custom interceptors get its
// guarantees for free: their panics are recovered, they see the request id
// in context, and domain errors they return are encoded into rich statuses
// like any handler error.
func WithUnaryInterceptor(i grpc.UnaryServerInterceptor) ServerOption {
	return func(c *serverConfig) { c.interceptors = append(c.interceptors, i) }
}

// NewServer creates a *grpc.Server with the standard interceptor chain.
// Register generated services on it as usual, then start it with Run (TCP)
// or Serve (any listener).
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

	chain := append(
		[]grpc.UnaryServerInterceptor{serverInterceptor(cfg.logger, codeFor)},
		cfg.interceptors...,
	)
	return grpc.NewServer(grpc.ChainUnaryInterceptor(chain...))
}

// shutdownTimeout bounds how long in-flight RPCs may finish after the
// context is cancelled; then their connections are closed and contexts
// cancelled. A var so tests can shrink it.
var shutdownTimeout = 10 * time.Second

// Run listens on TCP addr and serves until ctx is cancelled, then stops
// gracefully. Blocks; see Serve for the lifecycle and return values.
func Run(ctx context.Context, srv *grpc.Server, addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("grpckit: listen %s: %w", addr, err)
	}
	return Serve(ctx, srv, lis)
}

// Serve is Run on a listener the caller has already opened — a Unix socket,
// a named pipe, an inherited fd. Blocks until ctx is cancelled, then stops
// gracefully: in-flight RPCs get shutdownTimeout (10s) to finish; then their
// connections are closed and their contexts cancelled, and Serve waits for
// the handlers to return, so handlers must return when their ctx is
// cancelled. Returns nil after a cancel.
//
// Serve owns both lis and srv: it closes lis and stops srv when it returns,
// so each *grpc.Server goes to exactly one Serve call. If serving fails
// while ctx is live — the listener breaks, or srv is stopped from outside —
// Serve hard-stops srv and returns the error at once.
func Serve(ctx context.Context, srv *grpc.Server, lis net.Listener) error {
	addr := lis.Addr().String()
	served := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
			stopGracefully(srv)
		case <-served:
			srv.Stop() // close already-accepted connections too
		}
	}()

	err := srv.Serve(lis)
	close(served)
	<-stopped
	if ctx.Err() != nil {
		return nil
	}
	if err == nil { // srv.Serve returns nil when srv is stopped from outside
		err = grpc.ErrServerStopped
	}
	return fmt.Errorf("grpckit: serve %s: %w", addr, err)
}

// stopGracefully is GracefulStop with a shutdownTimeout grace period; after
// it, Stop cancels the remaining RPCs and GracefulStop still waits for their
// handlers to return.
func stopGracefully(srv *grpc.Server) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.GracefulStop()
	}()

	timer := time.NewTimer(shutdownTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		srv.Stop() // closes the connections GracefulStop is waiting on
		<-done
	}
}
