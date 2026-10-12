# Service Runtime: Composing main.go

The four packages every `main.go` is built from, and how they compose. Each has its own guide with the full API and anti-patterns — this page is the assembly view.

| Package | Job | Guide |
|---|---|---|
| [`app`](../../app/README.md) | lifecycle: concurrent adapters, shutdown on signal/failure, reverse-order cleanup | [app.md](./app.md) |
| [`config`](../../config/README.md) | typed env config, `.env`/`.env.local`, defaults + required + validation, optional flag binding | [config.md](./config.md) |
| [`observability`](../../observability/README.md) | the service logger (slog, env-configured); metrics/tracing land here later | [observability.md](./observability.md) |
| [`health`](../../health/README.md) | self-diagnosing checks: deep `/healthz`, dumb `/livez`, gRPC aggregate | [health.md](./health.md) |

These four are `main()`'s concern only — features never import them.

## A complete main.go

```go
func main() {
	// 1. observe first — everything after this logs through the service logger
	logger := observability.NewLogger("myservice")
	slog.SetDefault(logger)

	// 2. config: loaded once, passed down as values
	cfg := config.MustLoad[AppConfig]()
	ctx := context.Background()

	// 3. infra
	db := postgres.MustConnect(ctx, cfg.DatabaseURL)
	migrations, _ := fs.Sub(migrationsFS, "migrations")
	if err := db.Migrate(ctx, migrations); err != nil {
		logger.Error("migrations failed", "error", err)
		os.Exit(1)
	}
	pub := events.NewChannelPublisher(events.WithLogger(logger))

	// 4. features (each New wires ports to adapters and subscribes handlers)
	couponFeature := coupon.New(db, pub)

	// 5. transports
	r := httpapi.NewRouter(httpapi.WithLogger(logger))
	couponFeature.RegisterRoutes(r.Group("/api"))
	srv := grpckit.NewServer(grpckit.WithLogger(logger))
	couponsv1.RegisterCouponServiceServer(srv, couponFeature.GRPCController)

	// 6. health — one check per infra dependency
	checker := health.NewChecker(health.Check{Name: "postgres", Check: db.Ping})
	r.GET("/livez", gin.WrapH(health.Liveness()))
	r.GET("/healthz", gin.WrapH(checker.Handler()))
	checker.RegisterGRPC(srv)

	// 7. run — registration order is dependency order
	if err := app.Run(ctx,
		app.Adapter{Name: "postgres", Close: func(context.Context) error { db.Close(); return nil }},
		app.Adapter{Name: "events", Run: pub.Run, Close: pub.Close}, // before the servers: drains after them
		app.Adapter{Name: "http", Run: func(ctx context.Context) error { return httpapi.Run(ctx, r, cfg.HTTPAddr) }},
		app.Adapter{Name: "grpc", Run: func(ctx context.Context) error { return grpckit.Run(ctx, srv, cfg.GRPCAddr) }},
	); err != nil {
		logger.Error("service failed", "error", err)
		os.Exit(1)
	}
}
```

The single-binary variant swaps step 3: `sqlite.MustOpen` + `cache.NewInMemory()` — steps 1–2 and 4–7 are identical (see [sqlite](./sqlite.md)).

## The ordering rules worth memorizing

1. **Logger before everything** — even config errors should come out structured.
2. **Migrations before features** — a feature must never see a stale schema.
3. **Adapter registration is dependency order** (db → events → servers); reverse-order `Close` then drains the publisher *after* in-flight requests finish publishing and closes the pool last.
4. **One health check per infra dependency** — every kernel client exposes `Ping`; forgetting one is a silent blind spot on `/healthz`.
