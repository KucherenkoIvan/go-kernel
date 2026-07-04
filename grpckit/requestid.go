package grpckit

import (
	"context"

	"google.golang.org/grpc/metadata"

	"github.com/KucherenkoIvan/go-kernel/ddd"
)

// requestIDKey is the gRPC metadata key used for request-id propagation.
const requestIDKey = "x-request-id"

type requestIDCtxKey struct{}

// RequestID returns the request id propagated through the gRPC metadata,
// or "" if there is none.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDCtxKey{}).(string)
	return id
}

// WithRequestID puts a request id into the context; the client interceptor
// forwards it as gRPC metadata on outgoing calls.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDCtxKey{}, id)
}

// ensureRequestID extracts the request id from incoming metadata or
// generates a new one — every handler runs with a request id in context.
func ensureRequestID(ctx context.Context) context.Context {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if values := md.Get(requestIDKey); len(values) > 0 && values[0] != "" {
			return WithRequestID(ctx, values[0])
		}
	}
	return WithRequestID(ctx, ddd.NewEntityID())
}

// injectRequestID forwards the context's request id as outgoing metadata.
func injectRequestID(ctx context.Context) context.Context {
	if id := RequestID(ctx); id != "" {
		return metadata.AppendToOutgoingContext(ctx, requestIDKey, id)
	}
	return ctx
}
