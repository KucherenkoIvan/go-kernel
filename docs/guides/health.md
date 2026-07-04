# health: Self-Diagnosing Checks

Every infra adapter registers a named check; the service reports per-dependency status with details — not just "the pod is reachable". Package readme: [`health/README.md`](../../health/README.md).

## Liveness vs readiness — the split that matters

| Endpoint | Question | Behavior |
|---|---|---|
| `/livez` — `health.Liveness()` | is the process up? | always 200 |
| `/healthz` — `checker.Handler()` | can it do useful work? | 200 / 503 + per-check details |

They are deliberately separate: if the **liveness** probe ran the deep checks, a postgres outage would make the orchestrator restart your pods in a loop — fixing nothing and killing in-flight state. Only `/healthz` (readiness) should gate traffic.

## Wiring

```go
checker := health.NewChecker(
	health.Check{Name: "postgres",       Check: db.Ping},       // postgres.Client
	health.Check{Name: "kafka-producer", Check: producer.Ping}, // kafka.Producer
	health.Check{Name: "kafka-consumer", Check: consumer.Ping}, // kafka.Consumer
	health.Check{Name: "redis",          Check: store.Ping},    // cache.Redis
)

r.GET("/livez", gin.WrapH(health.Liveness()))
r.GET("/healthz", gin.WrapH(checker.Handler()))
checker.RegisterGRPC(srv) // standard gRPC health service — aggregate only
```

**Every kernel client exposes a compatible `Ping`** (`postgres`, `sqlite`, `kafka.Producer`, `kafka.Consumer`, `cache.Redis`) — registering them is one line each, and forgetting one is a silent blind spot. Custom dependencies fit the same shape: `health.Check{Name: "s3", Check: func(ctx context.Context) error { ... }}`.

A Run-adapter that *crashes* needs no check — `app.Run` shuts the service down, which the orchestrator sees. But a Run-adapter that *degrades without dying* does: the Kafka consumer deliberately survives broker outages (fetch errors are logged, not fatal), so only its `Ping` reveals "running but consuming nothing".

## The report

Checks run **in parallel** with a 5s per-check timeout. A healthy 200 still lists **every registered check** with its positive status — one curl shows the whole dependency picture:

```json
{"status":"ok","checks":{"postgres":{"status":"ok"},"kafka":{"status":"ok"},"redis":{"status":"ok"}}}
```

Degraded → 503, failing checks carry the error:

```json
{"status":"degraded","checks":{"postgres":{"status":"down","error":"connection refused"},"kafka":{"status":"ok"},"redis":{"status":"ok"}}}
```

The gRPC health protocol carries only `SERVING`/`NOT_SERVING` — the detailed report lives on HTTP; gRPC reflects the aggregate.

## Anti-patterns

| Anti-pattern | Why it is bad | The right way |
|-------------|-------------|---------------|
| Deep checks on the liveness probe | Restart loops that fix nothing | `/livez` dumb, `/healthz` deep |
| An infra adapter without a check | Silent blind spot on `/healthz` | One `health.Check` per dependency, always |
| Checks with side effects or writes | The probe becomes load | `Ping`-class reads only |
| Caching check results "for performance" | Probes exist to detect change | Checks are cheap and parallel; let them run |
| Exposing `/healthz` publicly unauthenticated | Leaks infra details (error strings) | Internal port / ingress rule |
