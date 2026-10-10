# grpckit

gRPC servers and clients with the kernel's standard interceptor chain — services never hand-write the serialize/send or deserialize/route path.

- `NewServer` — request-id, panic recovery, logging, and domain-error encoding built in; `WithErrorCode` maps domain codes to gRPC statuses; `WithUnaryInterceptor` appends custom interceptors (auth, metrics) *inside* the standard chain
- `Run` — listen on a TCP address, serve until ctx cancel, then graceful stop
- `Serve` — the same on a listener you opened yourself (Unix socket, named pipe, inherited fd); `Run` is `net.Listen("tcp", addr)` + `Serve`
- `Connect` / `MustConnect` — client with request-id propagation and error decoding
- `RemoteDomainError` — a domain error received over the wire; embeds `ddd.DomainError`, so `ddd.IsDomainError` works **across service boundaries**
- Anything non-domain (including panics) reaches clients as a masked `Internal`

```go
srv := grpckit.NewServer(grpckit.WithErrorCode("coupon_not_found", codes.NotFound))
couponsv1.RegisterCouponServiceServer(srv, controller)
err := grpckit.Run(ctx, srv, cfg.GRPCAddr)

// local API over a Unix socket; clients dial "unix://" + path
lis, _ := net.Listen("unix", "/run/myapp/api.sock")
err = grpckit.Serve(ctx, srv, lis)
```

In-memory test round-trips live in [`grpckittest`](grpckittest/) (`Serve(t, srv)` → connected client over bufconn). Contracts (buf-managed protos + committed generated code) live in [contracts/](../contracts/).

Guides: [docs/guides/grpc.md](../docs/guides/grpc.md), [docs/guides/middleware-auth.md](../docs/guides/middleware-auth.md)
