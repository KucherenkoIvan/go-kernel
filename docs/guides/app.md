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

## Two-stage shutdown: the second signal kills

The first SIGINT/SIGTERM is `app.Run`'s: it logs `shutting down; signal again to force quit` and starts the graceful shutdown above. From then on the signal handler is released, so the **next** SIGINT/SIGTERM gets the signal's default action — the process dies at once, `Close` or no `Close`. An operator facing a shutdown that hangs (a `Close` that ignores its context, a stuck network teardown) presses Ctrl-C again instead of reaching for `kill -9`.

The handler is released whenever shutdown begins, not only on a signal: after an adapter failure or a parent-ctx cancellation, the **first** SIGINT/SIGTERM already force-quits. There is nothing left for a signal to start gracefully at that point — swallowing it would only take away the way out of a hung `Close`. The 15s close budget still applies to every `Close` that honors its context.

Two consequences of using the default action:

- If the program has its own `signal.Notify` (or `signal.NotifyContext`) for SIGINT/SIGTERM, later signals go to that handler instead of killing the process — the program owns them then.
- A process started with SIGINT ignored (a background job of a non-interactive shell) goes back to ignoring it once released; SIGTERM still force-quits.

## Logger

`Run` logs adapter started/stopped/closed and the first signal through `slog.Default()`. A process that keeps its logger out of the global default injects it with `RunWith`:

```go
logger := slog.New(slog.NewTextHandler(os.Stderr, nil)).With("component", "mydaemon")
err := app.RunWith(ctx, app.Options{Logger: logger}, adapters...)
```

`Run(ctx, adapters...)` is `RunWith(ctx, app.Options{}, adapters...)`; a nil `Logger` means `slog.Default()`, read when `RunWith` starts.

## Registration order is dependency order

Register dependencies before dependents; reverse-order `Close` then releases dependents first:

```go
app.Adapter{Name: "postgres", Close: ...}, // 1st registered → closed last
app.Adapter{Name: "events",   Run: pub.Run, Close: pub.Close}, // drains after servers stop publishing
app.Adapter{Name: "http",     Run: ...},   // last registered → effectively stops first
```

Getting this wrong is silent until shutdown: an event publisher registered *after* the servers drains **before** in-flight requests finish publishing — events lost on every deploy.

## Testing

Adapters are plain functions, so lifecycle logic is unit-testable — see [`app/app_test.go`](../../app/app_test.go) for the patterns (error propagation, reverse close order, graceful cancel, the injected logger). Real signals are tested in [`app/signal_test.go`](../../app/signal_test.go): a second signal would kill the test binary, so the test re-executes itself as a subprocess and asserts on its exit status.

## Anti-patterns

| Anti-pattern | The right way |
|-------------|---------------|
| Hand-rolled signal handling / WaitGroups in main | `app.Run` |
| `Run` that doesn't respect ctx cancellation | Every loop selects on `<-ctx.Done()` |
| Resources closed by defer in main *and* an adapter Close | One owner: the adapter |
| Publisher/relay registered after the servers | Dependencies first — reverse close does the rest |
| Ignoring the returned error | It carries both run and close failures — log and exit non-zero |
| Extra `signal.Notify` in main "to be safe" around `app.Run` | `app.Run` owns SIGINT/SIGTERM; a second handler swallows the force-quit signal |
| `slog.SetDefault` only to route `app`'s lines to a custom logger | `app.RunWith(ctx, app.Options{Logger: logger}, ...)` |
