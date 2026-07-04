package grpckit

import (
	"context"
	"log/slog"
	"runtime/debug"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/KucherenkoIvan/go-kernel/ddd"
)

// serverInterceptor is the standard server chain: request-id, panic
// recovery, error logging, domain-error encoding.
func serverInterceptor(logger *slog.Logger, codeFor func(string) codes.Code) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp any, err error) {
		ctx = ensureRequestID(ctx)
		log := logger.With("method", info.FullMethod, "requestId", RequestID(ctx))

		defer func() {
			if r := recover(); r != nil {
				log.Error("panic in grpc handler", "panic", r, "stack", string(debug.Stack()))
				resp, err = nil, status.Error(codes.Internal, "internal error")
			}
		}()

		resp, err = handler(ctx, req)
		if err != nil {
			if ddd.IsDomainError(err) {
				log.Debug("domain error", "code", err.Error())
			} else {
				log.Error("handler error", "error", err)
			}
			err = encodeError(err, codeFor)
		}
		return resp, err
	}
}

// clientInterceptor is the standard client chain: request-id propagation and
// domain-error decoding.
func clientInterceptor() grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		ctx = injectRequestID(ctx)
		return decodeError(invoker(ctx, method, req, reply, cc, opts...))
	}
}
