# observability

How services are observed. Logging today; metrics and tracing will land in this same package, so imports never churn.

- `NewLogger(service)` — slog logger on stdout configured from the environment (`LOG_LEVEL`: debug/info/warn/error, `LOG_FORMAT`: json/text), with a `service` attribute on every line
- `NewLoggerWith(service, Options{Writer, Level, Format})` — the same with the destination, level and format chosen by the caller; a zero field keeps the env-driven default, an unknown `Format` is an error

```go
logger := observability.NewLogger("myservice")
slog.SetDefault(logger) // kernel packages log through slog.Default()
```

```go
// a daemon: stderr plus its own sink, level from its config, never from LOG_LEVEL
var level slog.LevelVar // runtime-adjustable
logger, err := observability.NewLoggerWith("mydaemon", observability.Options{
	Writer: io.MultiWriter(os.Stderr, sink),
	Level:  &level,
	Format: "text",
})
```

Guide: [docs/guides/observability.md](../docs/guides/observability.md)
