// Package auth defines the transport-agnostic identity container.
//
// Authentication middleware (HTTP or gRPC) verifies credentials and puts a
// Principal into the context; adapters read it and pass typed values into
// use-cases. Token verification itself is a per-project concern — the kernel
// only standardizes where the identity lives, so both transports and all
// features agree on it.
//
// Use-cases never read the Principal from context: adapters extract it and
// pass explicit arguments (e.g. domain.UserID). Fine-grained business
// authorization stays a Gateway port inside use-cases.
package auth

import "context"

// Principal is the authenticated caller.
type Principal struct {
	UserID string
	Roles  []string
}

// HasRole reports whether the principal has the given role.
func (p Principal) HasRole(role string) bool {
	for _, r := range p.Roles {
		if r == role {
			return true
		}
	}
	return false
}

type ctxKey struct{}

// WithPrincipal puts the authenticated principal into the context —
// called by authentication middleware after verifying credentials.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// PrincipalFrom returns the authenticated principal, if any.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}
