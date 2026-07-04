# httpapi

JSON REST APIs on Gin with the same guarantees as `grpckit`, HTTP edition. The containment rule: `gin.Context` never leaves a feature's `adapters/httpapi/`.

- `NewRouter` — a plain `*gin.Engine` with the standard chain installed: request-id → recovery → error mapping; plain Gin from there on (groups, ecosystem middleware)
- `Fail(c, err)` — the single failure path; the middleware maps `*HTTPError` → its status, domain errors → 400 or the `WithErrorStatus` mapping, everything else → masked 500
- Uniform error body: `{"error":{"code":"coupon_exhausted","requestId":"..."}}`
- `BadRequest` / `Unauthorized` / `Forbidden` / `NotFound` / `NewError` — transport-level errors
- `Run` — serve until ctx cancel, then graceful shutdown

```go
r := httpapi.NewRouter(httpapi.WithErrorStatus("coupon_not_found", http.StatusNotFound))
couponHandlers.RegisterRoutes(r.Group("/api"))
err := httpapi.Run(ctx, r, cfg.HTTPAddr)
```

A `grpckit.RemoteDomainError` from a downstream call maps to a clean 4xx automatically — domain errors flow service→service→client with zero re-mapping code.

Guides: [docs/guides/http.md](../docs/guides/http.md), [docs/guides/middleware-auth.md](../docs/guides/middleware-auth.md)
