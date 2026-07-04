# Middleware & Auth

How cross-cutting concerns (authentication, coarse authorization, rate limiting, metrics) plug into both transports, and where auth logic belongs.

## The split: guards vs gateways

Authorization happens at two levels, and confusing them is the classic mistake:

| | Question | Where | Mechanism |
|---|---|---|---|
| **Authentication + coarse authorization** | Who is calling? May they reach this route/method at all? | Transport middleware | gRPC interceptor / Gin middleware |
| **Business authorization** | May *this user* do *this* to *this object*? | Application layer | A Gateway port called inside the use-case |

Middleware answers the first question and puts a `Principal` into the context. Use-cases answer the second through a port (e.g. `PermissionGateway`) — they **never** read the principal from context. Adapters extract it and pass typed arguments:

```go
p, _ := auth.PrincipalFrom(ctx)
err := h.useCoupon.Execute(ctx, domain.UserID(p.UserID), couponID)
```

This keeps use-cases transport-agnostic and trivially testable — no context fixtures, just arguments.

## The Principal

The [`auth`](../../auth/) package standardizes *where identity lives*, nothing more:

```go
type Principal struct {
	UserID string
	Roles  []string
}

auth.WithPrincipal(ctx, p)        // set — called by authentication middleware
auth.PrincipalFrom(ctx)           // get — called by adapters
p.HasRole("admin")                // coarse role check
```

Token verification (JWT parsing, key management, sessions) is a per-project concern — implement a `TokenVerifier` in the service's `shared/infra/auth/` and hand it to the middleware. Both transports set the same context value, so a handler, a use-case log line, or a downstream adapter sees identical identity regardless of how the request arrived.

## gRPC: custom interceptors

`grpckit.NewServer` accepts custom interceptors that run **inside** the standard chain:

```go
srv := grpckit.NewServer(
	grpckit.WithLogger(logger),
	grpckit.WithUnaryInterceptor(AuthInterceptor(verifier)),   // order preserved
	grpckit.WithUnaryInterceptor(MetricsInterceptor(registry)),
)
```

### Ordering semantics

The standard chain (request-id → recovery → logging → error encoding) is outermost. Custom interceptors therefore inherit its guarantees:

- a panic inside a custom interceptor is recovered into a masked `Internal`;
- the request id is already in context when a custom interceptor runs;
- a **domain error returned by a custom interceptor is encoded** into a rich status exactly like a handler error — an auth interceptor may return `&UserBannedError{}` and the caller receives a `RemoteDomainError` with code `user_banned`.

### Auth interceptor

```go
func AuthInterceptor(verifier TokenVerifier) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if isExempt(info.FullMethod) { // health checks etc. — method-name matching
			return handler(ctx, req)
		}
		md, _ := metadata.FromIncomingContext(ctx)
		principal, err := verifier.Verify(ctx, bearerToken(md))
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "unauthenticated")
		}
		return handler(auth.WithPrincipal(ctx, principal), req)
	}
}
```

gRPC has no route groups — exemptions and per-service policies are `info.FullMethod` checks (the generated `<Service>_<Method>_FullMethodName` constants keep this typo-safe).

## HTTP: Gin middleware

Middleware scoping is native and per route group — this is where Gin earns its keep:

```go
r := httpapi.NewRouter(httpapi.WithLogger(logger)) // standard chain already installed

public := r.Group("/api")
public.GET("/coupons/:id", h.GetCoupon)                    // anonymous

authed := r.Group("/api", AuthMiddleware(verifier))        // authentication boundary
authed.POST("/coupons/:id/use", h.UseCoupon)

admin := authed.Group("/admin", RequireRole("admin"))      // stacked coarse authorization
admin.POST("/coupons", h.CreateCoupon)
```

### Auth middleware

```go
func AuthMiddleware(verifier TokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal, err := verifier.Verify(c.Request.Context(), bearerToken(c.GetHeader("Authorization")))
		if err != nil {
			httpapi.Fail(c, httpapi.Unauthorized("unauthenticated"))
			return
		}
		// into the request context, NOT gin.Context keys —
		// downstream code stays gin-free and transport-agnostic
		c.Request = c.Request.WithContext(auth.WithPrincipal(c.Request.Context(), principal))
		c.Next()
	}
}

func RequireRole(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if p, ok := auth.PrincipalFrom(c.Request.Context()); !ok || !p.HasRole(role) {
			httpapi.Fail(c, httpapi.Forbidden("access_denied"))
			return
		}
		c.Next()
	}
}
```

Failures go through `httpapi.Fail` like everywhere else, so auth errors follow the uniform error contract. Third-party Gin middleware (CORS, rate limiting, compression) composes the same way — the kernel imposes nothing beyond the standard chain being first.

## Identity across service hops

A request id set at the HTTP edge travels automatically: `httpapi` puts it in context → `grpckit.Connect`'s client interceptor forwards it as metadata → the next service's server chain picks it up. The principal does **not** hop automatically — and should not: each service authenticates its own callers. If a downstream service needs the end-user identity, make it an explicit contract field or a signed token argument, not ambient context.

## Both transports, end to end

The auth patterns above are exercised as tests, which double as reference implementations:

- gRPC: [`grpckit/interceptor_ext_test.go`](../../grpckit/interceptor_ext_test.go) — token check, principal reaching the handler, domain error from an interceptor crossing the wire, panic recovery.
- HTTP: [`httpapi/httpapi_test.go`](../../httpapi/httpapi_test.go) (`TestAuthMiddleware_Pattern`) — 401 without token, principal + role visible to the handler with one.

## Anti-patterns

| Anti-pattern | Why it is bad | The right way |
|-------------|-------------|---------------|
| Use-case reads `auth.PrincipalFrom(ctx)` | Business logic depends on ambient transport state | Adapter extracts, passes `domain.UserID` as an argument |
| Business authorization in middleware | "May the user edit *this* coupon" needs domain data | `PermissionGateway` port inside the use-case |
| Identity in `gin.Context` keys (`c.Set/c.Get`) | Invisible to non-Gin code, ties adapters to Gin | `auth.WithPrincipal` on `c.Request.Context()` |
| Custom gRPC interceptor placed outside the standard chain | Loses recovery/error-encoding guarantees | `grpckit.WithUnaryInterceptor` (always inside) |
| Forwarding the caller's token to downstream services | Confused-deputy; each service must authenticate its own callers | Explicit identity fields in contracts |
| Hand-rolled 401/403 JSON in middleware | Breaks the uniform error contract | `httpapi.Fail` + `Unauthorized`/`Forbidden` |
