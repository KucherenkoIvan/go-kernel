# observability

How services are observed. Logging today; metrics and tracing will land in this same package, so imports never churn.

- `NewLogger(service)` — slog logger configured from the environment (`LOG_LEVEL`: debug/info/warn/error, `LOG_FORMAT`: json/text), with a `service` attribute on every line

```go
logger := observability.NewLogger("myservice")
slog.SetDefault(logger) // kernel packages log through slog.Default()
```

Guide: [docs/guides/observability.md](../docs/guides/observability.md)
