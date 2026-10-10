# app: Service Lifecycle

How a service runs and shuts down. Package readme: [`app/README.md`](../../app/README.md). For the fully composed `main.go`, see [service runtime](./service-runtime.md).

## The Adapter

`app.Adapter` is anything the service runs or owns — deliberately transport-agnostic (no "server" in the vocabulary): HTTP/gRPC servers, the event publisher, Kafka consumers, the outbox relay, cron loops, the DB pool.

```go
type Adapter struct {
	Name  string                          // for logs and error messages
	Run   func(ctx context.Context) error // blocks until ctx cancel; nil for resources
	Close func(ctx context.Context) error // cleanup after all Runs return; nil if nothing to release
}
```

Both fields are optional:

```go
app.Adapter{Name: "postgres", Close: func(context.Context) error { db.Close(); return nil }} // resource: Close-only
app.Adapter{Name: "http", Run: func(ctx context.Context) error { return httpapi.Run(ctx, r, addr) }} // server: Run-only
app.Adapter{Name: "events", Run: pub.Run, Close: pub.Close}                                  // both
```

A server on a listener other than TCP (a Unix socket for a local API, say) opens the listener inside its `Run`, so a listen failure is that adapter's error: see [gRPC: any listener](./grpc.md#any-listener-unix-sockets-named-pipes).

## Semantics

```go
err := app.Run(ctx, adapters...)
```

1. All `Run`s start **concurrently**.
2. The shared context is cancelled by SIGINT/SIGTERM, by the parent ctx, or by the first `Run` returning an error — everything then shuts down. Kernel `Run` functions (`httpapi.Run`, `grpckit.Run`/`Serve`, `ChannelPublisher.Run`, `Relay.Run`, `Consumer.Run`) finish in-flight work and return nil on cancellation.
3. After **all** `Run`s have returned, `Close` runs in **reverse registration order**, with a 15s total budget.
4. The returned error joins run and close failures; nil means a clean shutdown.

A `Run` that returns nil early (a one-shot task) does not stop the service — only errors and signals do.

## Registration order is dependency order

Register dependencies before dependents; reverse-order `Close` then releases dependents first:

```go
app.Adapter{Name: "postgres", Close: ...}, // 1st registered → closed last
app.Adapter{Name: "events",   Run: pub.Run, Close: pub.Close}, // drains after servers stop publishing
app.Adapter{Name: "http",     Run: ...},   // last registered → effectively stops first
```

Getting this wrong is silent until shutdown: an event publisher registered *after* the servers drains **before** in-flight requests finish publishing — events lost on every deploy.

## Testing

Adapters are plain functions, so lifecycle logic is unit-testable — see [`app/app_test.go`](../../app/app_test.go) for the patterns (error propagation, reverse close order, graceful cancel).

## Anti-patterns

| Anti-pattern | The right way |
|-------------|---------------|
| Hand-rolled signal handling / WaitGroups in main | `app.Run` |
| `Run` that doesn't respect ctx cancellation | Every loop selects on `<-ctx.Done()` |
| Resources closed by defer in main *and* an adapter Close | One owner: the adapter |
| Publisher/relay registered after the servers | Dependencies first — reverse close does the rest |
| Ignoring the returned error | It carries both run and close failures — log and exit non-zero |
