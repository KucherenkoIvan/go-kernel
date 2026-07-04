# observability: Logging (and room to grow)

How services are observed. Logging only for now — metrics and tracing will land in this same package, so adopting them later is new calls, not new imports. Package readme: [`observability/README.md`](../../observability/README.md).

## Setup

The first two lines of every `main()`:

```go
logger := observability.NewLogger("myservice")
slog.SetDefault(logger)
```

Environment-driven, no config struct needed:

| Variable | Values | Default |
|---|---|---|
| `LOG_LEVEL` | `debug` `info` `warn` `error` | `info` |
| `LOG_FORMAT` | `json` `text` | `json` |

Every line carries `service=<name>`. `slog.SetDefault` matters: kernel packages (`app`, `events`, `grpckit`, `httpapi`, ...) default to `slog.Default()`, so one call routes everything — pass the logger explicitly (`WithLogger(...)` options) only when a component needs a different one.

## Conventions

- **Log with context.** Include the request id: `logger.With("requestId", requestid.From(ctx))`. The kernel's transport chains already do this for their own lines.
- **Errors are logged where they are handled**, once — the transport middleware logs handler failures; don't also log before returning the error (double logging is noise that looks like two incidents).
- Domain errors are `Debug` (expected business variants), real failures are `Error` — the kernel's middleware already applies this split; follow it in adapters.
- `debug` level and `text` format are for local development (`.env.local`); production runs `info`/`json`.

```go
// an adapter doing background work — the pattern for your own components:
log := logger.With("component", "report-generator", "requestId", requestid.From(ctx))
log.Info("report started", "reportId", id)
if err := generate(ctx); err != nil {
	log.Error("report failed", "reportId", id, "error", err)
}
```

## What lands here later

Metrics (Prometheus registry + the counters the outbox/relay/consumers should expose) and tracing (OTel setup, span helpers — the request-id propagation paths are exactly where trace context will ride). The package name is the promise: `observability.NewMeter`, `observability.NewTracer`, same import.

## Anti-patterns

| Anti-pattern | The right way |
|-------------|---------------|
| `fmt.Println` / `log.Printf` | slog everywhere — structure survives grep and ingestion |
| Logging the same error at every level of the stack | Log where handled; wrap with `fmt.Errorf("...: %w", err)` below |
| PII / tokens / payloads in log lines | Ids and codes, never contents |
| Per-package logger globals | `slog.Default()` set once in main; explicit `WithLogger` for exceptions |
| `debug` in production "temporarily" | `LOG_LEVEL` is env config — flip it per environment, not in code |
