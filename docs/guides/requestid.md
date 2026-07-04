# requestid: One Id Across Every Hop

A single request id that survives the whole path, so one grep over logs (or one error body) reconstructs a request's journey across services. Package readme: [`requestid/README.md`](../../requestid/README.md).

## The propagation map

```
HTTP request (x-request-id header, or generated)
  → httpapi middleware → context
    → use-case (logs carry it)
      → grpckit client → gRPC metadata → next service's context
      → outbox.Enqueue → outbox.headers → relay → Kafka headers
        → kafka.Consumer → handler context
          → ... and onward
```

All of this is automatic — `httpapi`, `grpckit`, `kafka`, and `outbox` handle their legs. This package exists so they agree on one context key and one wire name (`requestid.Header` = `x-request-id`).

## API

```go
ctx = requestid.With(ctx, id)   // put an id on the context
id := requestid.From(ctx)       // read it ("" if none)
ctx = requestid.Ensure(ctx)     // guarantee one (generates UUIDv7 if absent)
```

## When you touch it directly

Rarely — three cases:

**Seeding at a non-transport edge** (cron jobs, startup tasks, CLI commands — anything that *originates* work):

```go
func (j *ReportCron) tick(ctx context.Context) {
	ctx = requestid.Ensure(ctx) // this run's id; everything downstream inherits it
	// ...
}
```

**Logging in your own components:**

```go
log := logger.With("requestId", requestid.From(ctx))
```

**Tests asserting propagation:**

```go
ctx := requestid.With(context.Background(), "req-42")
// ... drive the flow, assert the far side saw "req-42"
```

Note the boundary with [`auth`](./middleware-auth.md): the request id hops across services automatically; the *principal deliberately does not* — each service authenticates its own callers.

## Anti-patterns

| Anti-pattern | The right way |
|-------------|---------------|
| A second correlation-id mechanism in one service | One package, one key — extend this one if needed |
| Passing the id as a function parameter | It is context baggage; that is what ctx is for |
| Using it as a business identifier or idempotency key | It is tracing metadata; use domain ids / event ids |
| Hand-forwarding headers between transports | The kernel chains already do it — check before wiring your own |
