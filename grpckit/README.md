# grpckit

gRPC servers and clients with the kernel's standard interceptor chain — services never hand-write the serialize/send or deserialize/route path.

- `NewServer` — request-id, panic recovery, logging, and domain-error encoding built in, for unary and streaming RPCs alike; `WithErrorCode` maps domain codes to gRPC statuses; `WithUnaryInterceptor` / `WithStreamInterceptor` append custom interceptors (auth, metrics) *inside* the standard chain
- `WithGRPCOptions` — any other `grpc.ServerOption` (keepalive, credentials, limits, stats handlers), appended after the standard chain; interceptors go through the two options above
- `StreamWithContext` — a `grpc.ServerStream` with its `Context()` replaced, for stream interceptors that hand the handler a derived context (e.g. a Principal)
- `Run` — listen on a TCP address, serve until ctx cancel, then graceful stop: in-flight RPCs get 10s, then their connections are closed and contexts cancelled, and Run waits for the handlers to return (handlers must honor ctx)
- `Serve` — the same on a listener you opened yourself (Unix socket, named pipe, inherited fd); `Run` is `net.Listen("tcp", addr)` + `Serve`. Serve owns the listener and the server: both are closed/stopped when it returns, and a listener failure returns at once
- `Connect` / `MustConnect` — client with request-id propagation and error decoding, on unary calls and streams (a stream's final status is decoded where `Recv` / `CloseAndRecv` returns it)
- An RPC ended by its own context (client cancel, deadline, the connection closed on shutdown), unary or streaming, is reported as `Canceled`/`DeadlineExceeded` instead of a masked `Internal`, and logged at debug (cancel) or warn (deadline, `Unavailable`) instead of as a handler error
- `RemoteDomainError` — a domain error received over the wire; embeds `ddd.DomainError`, so `ddd.IsDomainError` works **across service boundaries**
- Any other error (including panics) reaches clients as a masked `Internal`; gRPC status errors pass through unchanged

```go
srv := grpckit.NewServer(grpckit.WithErrorCode("coupon_not_found", codes.NotFound))
couponsv1.RegisterCouponServiceServer(srv, controller)
err := grpckit.Run(ctx, srv, cfg.GRPCAddr)
```

Any other listener — e.g. a local API on a Unix socket, which clients dial as `"unix://" + absolutePath`:

```go
lis, err := net.Listen("unix", "/run/myapp/api.sock")
if err != nil {
	return err
}
return grpckit.Serve(ctx, srv, lis)
```

Long-lived streams hold `Serve`'s shutdown for the full grace period unless their handler also watches the service's ctx and returns when it is cancelled — see the guide.

In-memory test round-trips live in [`grpckittest`](grpckittest/) (`Serve(t, srv)` → connected client over bufconn). Contracts (buf-managed protos + committed generated code) live in [contracts/](../contracts/).

Guides: [docs/guides/grpc.md](../docs/guides/grpc.md), [docs/guides/middleware-auth.md](../docs/guides/middleware-auth.md)
