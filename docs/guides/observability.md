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

## Choosing the destination, level and format

`NewLogger` is stdout and the two environment variables — right for a container whose stdout is collected. A process with its own log destinations (a daemon writing to stderr and to an in-process sink, a CLI that keeps stdout for its output) builds the logger with `NewLoggerWith`:

```go
type Options struct {
	Writer io.Writer    // nil: os.Stdout
	Level  slog.Leveler // nil: LOG_LEVEL (default info)
	Format string       // "json" | "text", case-insensitive; "": LOG_FORMAT (default json)
}
```

```go
var level slog.LevelVar
level.Set(cfg.LogLevel) // a slog.Level from the daemon's own config

logger, err := observability.NewLoggerWith("mydaemon", observability.Options{
	Writer: io.MultiWriter(os.Stderr, recentLines), // recentLines: any io.Writer the caller owns
	Level:  &level,                                  // later level.Set(...) changes it live
	Format: "text",
})
if err != nil {
	return err // only an unknown Format fails
}
slog.SetDefault(logger)
```

- **Each zero field keeps `NewLogger`'s behavior**, so `NewLoggerWith(name, Options{})` is exactly `NewLogger(name)`.
- **An explicit `Level` ignores `LOG_LEVEL` entirely.** A privileged daemon shouldn't take its verbosity from whatever environment it was started in; a `*slog.LevelVar` lets it change the level at runtime instead.
- **`Format` is checked; the environment is not.** An unknown `Options.Format` is an error (it came from the caller's config and should fail the start); an unknown `LOG_FORMAT` or `LOG_LEVEL` keeps falling back to json/info, as before.
- The kernel brings no sinks: a ring buffer, a file, a socket are the caller's `io.Writer`. slog's handlers serialize the writes of one logger and the loggers derived from it with `With`, so the writer needs locking of its own only if something else writes to it too.

To parse a level from a config string, `slog.Level` implements `encoding.TextUnmarshaler` (`"debug"`, `"INFO"`, `"warn+2"`).

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
| Hand-building a `slog.JSONHandler` in main to log somewhere other than stdout (and losing the `service` attribute) | `NewLoggerWith(service, Options{Writer: ...})` |
| A daemon whose verbosity is whatever `LOG_LEVEL` its environment carries | An explicit `Options.Level` (a `*slog.LevelVar` to change it live) |
