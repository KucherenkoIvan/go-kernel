package grpckit

import (
	"context"

	"google.golang.org/grpc/metadata"

	"github.com/KucherenkoIvan/go-kernel/requestid"
)

// RequestID returns the request id propagated through the gRPC metadata,
// or "" if there is none. Alias of requestid.From.
func RequestID(ctx context.Context) string {
	return requestid.From(ctx)
}

// WithRequestID puts a request id into the context; the client interceptor
// forwards it as gRPC metadata on outgoing calls. Alias of requestid.With.
func WithRequestID(ctx context.Context, id string) context.Context {
	return requestid.With(ctx, id)
}

// ensureRequestID extracts the request id from incoming metadata or
// generates a new one — every handler runs with a request id in context.
func ensureRequestID(ctx context.Context) context.Context {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if values := md.Get(requestid.Header); len(values) > 0 && values[0] != "" {
			return requestid.With(ctx, values[0])
		}
	}
	return requestid.Ensure(ctx)
}

// injectRequestID forwards the context's request id as outgoing metadata.
func injectRequestID(ctx context.Context) context.Context {
	if id := requestid.From(ctx); id != "" {
		return metadata.AppendToOutgoingContext(ctx, requestid.Header, id)
	}
	return ctx
}
