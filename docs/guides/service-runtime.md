# Service Runtime

The packages every `main.go` is built from: [`app`](../../app/) (lifecycle), [`config`](../../config/) (environment), [`observability`](../../observability/) (logging), [`health`](../../health/) (self-diagnosis). Together they make the composition root ~30 lines with correct shutdown, and they are the only place these concerns live — features never touch them.

## app: lifecycle

`app.Run` owns the process: starts every adapter, shuts everything down on SIGINT/SIGTERM or the first failure, cleans up in reverse registration order.

```go
err := app.Run(ctx,
	app.Adapter{Name: "postgres", Close: func(context.Context) error { db.Close(); return nil }},
	app.Adapter{Name: "events", Run: pub.Run, Close: pub.Close},
	app.Adapter{Name: "http", Run: func(ctx context.Context) error { return httpapi.Run(ctx, r, cfg.HTTPAddr) }},
	app.Adapter{Name: "grpc", Run: func(ctx context.Context) error { return grpckit.Run(ctx, srv, cfg.GRPCAddr) }},
)
```

An `Adapter` is anything the service runs or owns — servers, workers, the event publisher, the DB pool. Both fields are optional: servers are Run-only, resources are Close-only.

Semantics:

- All `Run`s start concurrently; a `Run` returning an error cancels the shared context and shuts the service down.
- SIGINT/SIGTERM cancels the same context — `httpapi.Run`/`grpckit.Run` finish in-flight work and return nil.
- After **all** `Run`s have returned, `Close` runs in **reverse registration order** with a 15s budget — register dependencies (db) before dependents (servers), and the event publisher before the servers so it drains last-published events.
- The returned error joins run and close failures; nil means a clean shutdown.

## config: environment

Typed config from env vars, loaded once in `main`, passed down as a value — never a global.

```go
type AppConfig struct {
	HTTPAddr    string        `env:"HTTP_ADDR" default:":8080"`
	GRPCAddr    string        `env:"GRPC_ADDR" default:":9090"`
	DatabaseURL string        `env:"DATABASE_URL" required:"true"`
	Timeout     time.Duration `env:"TIMEOUT" default:"5s"`
}

cfg := config.MustLoad[AppConfig]()
```

- Precedence: **OS environment > `.env.local` > `.env`** — files never override real env, so production is file-proof. `.env.local` is for personal overrides (gitignored); `.env` for shared dev defaults.
- Tags: `env` (variable name), `default`, `required:"true"`. Missing required variables are reported **all at once**, not one per restart.
- Supported types: string, bool, ints, float64, `time.Duration`, `[]string` (comma-separated). Nested structs group sections.
- Cross-field rules: implement `func (c *AppConfig) Validate() error`.

## observability: logging

```go
logger := observability.NewLogger("myservice")
slog.SetDefault(logger)
```

`LOG_LEVEL` (debug/info/warn/error) and `LOG_FORMAT` (json/text) come from the environment; every line carries `service`. Setting it as the slog default routes kernel packages (`app`, `events`, and any code using `slog.Default()`) through the same logger. Metrics and tracing will join this package later — same import, no churn.

## health: self-diagnosis

Every infra adapter registers a named check; the service reports per-dependency status with details — not just "the pod is reachable".

```go
checker := health.NewChecker(
	health.Check{Name: "postgres", Check: db.Ping},
	// health.Check{Name: "kafka", Check: producer.Ping},
)

r.GET("/livez", gin.WrapH(health.Liveness()))     // liveness probe: process is up, always 200
r.GET("/healthz", gin.WrapH(checker.Handler()))   // readiness probe: 200 / 503 + details
checker.RegisterGRPC(srv)                          // standard gRPC health service (aggregate only)
```

A healthy service answers 200 — and the body still lists **every registered check** with its positive status, so one curl shows the whole dependency picture, not just "ok":

```json
{"status":"ok","checks":{"postgres":{"status":"ok"},"kafka":{"status":"ok"}}}
```

When degraded, the failing checks carry details and the endpoint flips to 503:

```json
{"status":"degraded","checks":{"postgres":{"status":"down","error":"connection refused"},"kafka":{"status":"ok"}}}
```

**Every infra adapter registers a check** — the kernel clients all expose a compatible `Ping`: `postgres.Client.Ping`, `kafka.Producer.Ping`, `cache.Redis.Ping`, `sqlite.Client.Ping`. Each adapter guide shows its one-liner; forgetting one means a silent blind spot on this endpoint.

**Liveness and readiness are deliberately separate.** If the liveness probe ran the deep checks, a postgres outage would make the orchestrator restart your pods in a loop — fixing nothing and killing in-flight state. `/livez` answers "is the process up", `/healthz` answers "can it do useful work"; only the latter should gate traffic. The gRPC health protocol carries only SERVING/NOT_SERVING, so the detailed report lives on HTTP.

## A complete main.go

```go
func main() {
	logger := observability.NewLogger("myservice")
	slog.SetDefault(logger)

	cfg := config.MustLoad[AppConfig]()
	ctx := context.Background()

	// infra
	db := postgres.MustConnect(ctx, cfg.DatabaseURL)
	migrations, _ := fs.Sub(migrationsFS, "migrations")
	if err := db.Migrate(ctx, migrations); err != nil {
		logger.Error("migrations failed", "error", err)
		os.Exit(1)
	}
	pub := events.NewChannelPublisher(events.WithLogger(logger))

	// features
	couponFeature := coupon.New(db, pub)

	// transports
	r := httpapi.NewRouter(httpapi.WithLogger(logger))
	couponFeature.RegisterRoutes(r.Group("/api"))
	srv := grpckit.NewServer(grpckit.WithLogger(logger))
	couponsv1.RegisterCouponServiceServer(srv, couponFeature.GRPCController)

	// health
	checker := health.NewChecker(health.Check{Name: "postgres", Check: db.Ping})
	r.GET("/livez", gin.WrapH(health.Liveness()))
	r.GET("/healthz", gin.WrapH(checker.Handler()))
	checker.RegisterGRPC(srv)

	if err := app.Run(ctx,
		app.Adapter{Name: "postgres", Close: func(context.Context) error { db.Close(); return nil }},
		app.Adapter{Name: "events", Run: pub.Run, Close: pub.Close},
		app.Adapter{Name: "http", Run: func(ctx context.Context) error { return httpapi.Run(ctx, r, cfg.HTTPAddr) }},
		app.Adapter{Name: "grpc", Run: func(ctx context.Context) error { return grpckit.Run(ctx, srv, cfg.GRPCAddr) }},
	); err != nil {
		logger.Error("service failed", "error", err)
		os.Exit(1)
	}
}
```

## Anti-patterns

| Anti-pattern | The right way |
|-------------|---------------|
| Hand-rolled signal handling / WaitGroups in main | `app.Run` |
| Config read via `os.Getenv` scattered through the code | One struct, loaded once, passed down |
| Global config or global logger accessed from features | Constructor parameters; `slog.Default` only as the process-level sink |
| Deep checks on the liveness probe | `/livez` dumb, `/healthz` deep |
| `.env` files read in production | Files are a dev convenience; prod sets real env |
| Resources closed before dependents (db before servers) | Registration order: dependencies first — reverse close handles it |
