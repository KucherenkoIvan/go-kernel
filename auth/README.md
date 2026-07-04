# auth

The transport-agnostic identity container. Authentication middleware (HTTP or gRPC) verifies credentials and puts a `Principal` into the context; adapters read it and pass **typed arguments** into use-cases — which never touch the context themselves.

- `Principal` — the authenticated caller (`UserID`, `Roles`, `HasRole`)
- `WithPrincipal` / `PrincipalFrom` — context helpers, identical under both transports

```go
// middleware (either transport) after verifying the token:
ctx = auth.WithPrincipal(ctx, principal)

// adapter:
p, _ := auth.PrincipalFrom(ctx)
err := h.useCoupon.Execute(ctx, domain.UserID(p.UserID), couponID)
```

Token verification (JWT parsing, keys, sessions) is deliberately per-project — the kernel only standardizes *where identity lives*. Fine-grained business authorization stays a Gateway port inside use-cases.

Guide: [docs/guides/middleware-auth.md](../docs/guides/middleware-auth.md)
