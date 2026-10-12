# app

Service lifecycle: run all adapters concurrently, shut down on SIGINT/SIGTERM or the first failure, clean up in reverse registration order.

- `Adapter{Name, Run, Close}` — anything the service runs or owns; both fields optional (servers are Run-only, resources like the DB pool are Close-only)
- `Run(ctx, adapters...)` — blocks until shutdown; joins run and close errors; nil on a clean stop
- `RunWith(ctx, Options{Logger}, adapters...)` — `Run` with an injected `*slog.Logger` for the lifecycle lines (nil: `slog.Default()`)
- Two-stage shutdown: the first SIGINT/SIGTERM starts the graceful shutdown; once shutdown has begun (signal, ctx or adapter failure) the next one kills the process — the way out of a hung `Close`

```go
err := app.Run(ctx,
	app.Adapter{Name: "postgres", Close: func(context.Context) error { db.Close(); return nil }},
	app.Adapter{Name: "events", Run: pub.Run, Close: pub.Close},
	app.Adapter{Name: "http", Run: func(ctx context.Context) error { return httpapi.Run(ctx, r, cfg.HTTPAddr) }},
	app.Adapter{Name: "grpc", Run: func(ctx context.Context) error { return grpckit.Run(ctx, srv, cfg.GRPCAddr) }},
)
```

Registration order is dependency order: dependencies (db) before dependents (servers) — reverse-order `Close` handles the rest.

Guide: [docs/guides/app.md](../docs/guides/app.md) · Composition: [service runtime](../docs/guides/service-runtime.md)
