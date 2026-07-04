# HTTP (REST API)

JSON REST APIs on [Gin](https://github.com/gin-gonic/gin). The kernel side lives in [`httpapi`](../../httpapi/) — the HTTP mirror of `grpckit`: same request-id, same two-outcome error contract, same graceful lifecycle.

## Where HTTP sits in the architecture

The HTTP handler set is an adapter (`features/<feature>/adapters/httpapi/`) at the same altitude as the gRPC controller — a thin transport layer that binds input, calls use-cases, and returns results. A feature may expose both transports side by side:

```
features/coupon/adapters/
  grpc/coupon_controller.go
  httpapi/coupon_handlers.go
```

**The containment rule:** `gin.Context` never leaves `adapters/httpapi/`. Use-cases take `context.Context` and typed arguments. Everything Gin-specific — binding, routing, middleware — stays in the adapter, which is what keeps the router swappable and the application layer transport-agnostic.

## Quick start

```go
// cmd/<app>/main.go
r := httpapi.NewRouter(
	httpapi.WithLogger(logger),
	httpapi.WithErrorStatus("coupon_not_found", http.StatusNotFound),
)
couponHandlers.RegisterRoutes(r.Group("/api"))

err := httpapi.Run(ctx, r, cfg.HTTPAddr) // serves until ctx cancel, then graceful shutdown
```

`NewRouter` returns a plain `*gin.Engine` with the standard middleware chain installed (request-id → recovery → error mapping). From there on it is ordinary Gin — groups, custom middleware, the whole ecosystem.

Each feature exposes its routes from the adapter; the composition root mounts them:

```go
// features/coupon/adapters/httpapi/coupon_handlers.go
func (h *CouponHandlers) RegisterRoutes(r gin.IRouter) {
	r.GET("/coupons/:id", h.GetCoupon)
	r.POST("/coupons/:id/use", h.UseCoupon)
}
```

## Handlers

A handler does three things: bind/validate, call the use-case, respond. Failures go through `httpapi.Fail` — never hand-written error JSON:

```go
type useCouponRequest struct {
	UserID string `json:"userId" binding:"required,uuid"`
}

func (h *CouponHandlers) UseCoupon(c *gin.Context) {
	var req useCouponRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.Fail(c, httpapi.BadRequest("invalid_request", err))
		return
	}

	err := h.useCoupon.Execute(c.Request.Context(),
		domain.UserID(req.UserID), domain.CouponID(c.Param("id")))
	if err != nil {
		httpapi.Fail(c, err) // domain errors map themselves — see below
		return
	}
	c.Status(http.StatusNoContent)
}
```

DTOs live in the adapter and are validated at bind time with `binding` tags ([validator/v10](https://github.com/go-playground/validator) — the analog of input-DTO validation at the transport). Convert to domain types immediately after binding.

## Error contract

Every error response has one shape:

```json
{"error": {"code": "coupon_exhausted", "requestId": "0190..."}}
```

`httpapi.Fail(c, err)` records the error; the middleware maps it by precedence:

| Error | Response |
|---|---|
| `*httpapi.HTTPError` (`BadRequest`, `Unauthorized`, `Forbidden`, `NotFound`, `NewError`) | its status + code |
| ddd domain error (`ddd.IsDomainError`) | 400 (or the `WithErrorStatus` mapping) + the domain code |
| anything else, or a panic | 500 `"internal_error"` — details logged server-side, never sent |

Two properties worth knowing:

- **Domain errors from downstream gRPC calls map automatically.** `grpckit.RemoteDomainError` embeds `ddd.DomainError`, so a use-case that propagates a domain error received from another service still produces a clean 4xx with the original code — no re-mapping code anywhere.
- **Internals never leak.** Infrastructure errors and panics reach clients only as `"internal_error"`; the real error goes to the log with the request id.

## Request id

Every request runs with an id in `c.Request.Context()` — taken from the `x-request-id` header or generated — and the response echoes it in the same header. It is shared with `grpckit` through the [`requestid`](../../requestid/) package, so the id survives HTTP → use-case → gRPC hops and appears in every log line and error body along the way.

## Custom middleware & auth

Standard Gin composition — see the [middleware & auth guide](./middleware-auth.md) for the full pattern across both transports.

## Testing

Plain `httptest`, no network — see [`httpapi/httpapi_test.go`](../../httpapi/httpapi_test.go) for the full pattern:

```go
r := httpapi.NewRouter(httpapi.WithLogger(discard))
handlers.RegisterRoutes(r.Group("/api"))

req := httptest.NewRequest("POST", "/api/coupons/c-1/use", body)
w := httptest.NewRecorder()
r.ServeHTTP(w, req)
```

## Anti-patterns

| Anti-pattern | The right way |
|-------------|---------------|
| `gin.Context` in application or domain code | Handlers extract typed values; use-cases take `context.Context` |
| Hand-written error JSON in handlers | `httpapi.Fail(c, err)` + the uniform contract |
| Mapping domain errors to statuses in handlers | Return the domain error; configure `WithErrorStatus` once |
| Business logic in handlers | Handlers bind, call a use-case, respond |
| Reading `gin.Context` keys for identity | Principal lives in `c.Request.Context()` via the `auth` package |
| Raw `binding`-less DTO fields | Every input field carries a `binding` tag |
