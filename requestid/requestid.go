// Package requestid carries a request id through context, shared by all
// transports (grpckit, httpapi) so an id set at any edge survives every hop:
// HTTP in → use-case → gRPC out.
package requestid

import (
	"context"

	"github.com/KucherenkoIvan/go-kernel/ddd"
)

// Header is the wire name of the request id in both HTTP headers and gRPC
// metadata.
const Header = "x-request-id"

type ctxKey struct{}

// With puts a request id into the context.
func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// From returns the request id from the context, or "" if there is none.
func From(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

// Ensure returns a context that is guaranteed to carry a request id,
// generating one (UUIDv7) if absent.
func Ensure(ctx context.Context) context.Context {
	if From(ctx) != "" {
		return ctx
	}
	return With(ctx, ddd.NewEntityID())
}
