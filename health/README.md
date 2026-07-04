# health

Self-diagnosing health checks: every infra adapter registers a named check, and the service reports per-dependency status with details — not just "the pod is reachable". Liveness and readiness are deliberately separate endpoints.

- `Check{Name, Check}` — one named dependency diagnosis (`db.Ping`, `producer.Ping`, `store.Ping`, ...)
- `NewChecker(checks...)` + `Handler()` — `/healthz`: checks run in parallel with a timeout; 200 (listing **every** check with its positive status) or 503 with details
- `Liveness()` — `/livez`: always 200, "the process is up"
- `RegisterGRPC(srv)` — the standard gRPC health service, reflecting the aggregate

```go
checker := health.NewChecker(
	health.Check{Name: "postgres", Check: db.Ping},
	health.Check{Name: "kafka", Check: producer.Ping},
)
r.GET("/livez", gin.WrapH(health.Liveness()))
r.GET("/healthz", gin.WrapH(checker.Handler()))
checker.RegisterGRPC(srv)
```

```json
{"status":"ok","checks":{"postgres":{"status":"ok"},"kafka":{"status":"ok"}}}
```

Guide: [docs/guides/health.md](../docs/guides/health.md)
