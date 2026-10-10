package grpckit

import (
	"context"

	"google.golang.org/grpc"
)

// StreamWithContext returns ss with its Context replaced by ctx — the way a
// stream interceptor hands the handler a derived context (a Principal, a
// deadline), since grpc.StreamHandler takes no context argument:
//
//	return handler(srv, grpckit.StreamWithContext(auth.WithPrincipal(ss.Context(), p), ss))
//
// Derive ctx from ss.Context(), so the request id and cancellation carry over.
func StreamWithContext(ctx context.Context, ss grpc.ServerStream) grpc.ServerStream {
	return &contextServerStream{ServerStream: ss, ctx: ctx}
}

type contextServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *contextServerStream) Context() context.Context { return s.ctx }
