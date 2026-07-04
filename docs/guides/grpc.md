# gRPC

Synchronous communication between services. Contract-first: protos live in [`contracts/`](../../contracts/), generated code is committed and imported from the module. The runtime side lives in [`grpckit`](../../grpckit/).

## Contracts workflow

1. Add/edit a proto under `contracts/proto/<domain>/v1/`.
2. `buf lint && buf generate` (see [contracts/README.md](../../contracts/README.md) for one-time tooling setup).
3. Commit `proto/` and `gen/` together.

Consumers never run codegen — they import the generated package:

```go
import couponsv1 "github.com/KucherenkoIvan/go-kernel/contracts/gen/coupons/v1"
```

## Server side

The gRPC controller is an adapter (`features/<feature>/adapters/grpc/`): thin, maps proto types to use-case calls, returns domain errors as-is — the interceptor chain does the rest.

```go
// features/coupon/adapters/grpc/coupon_controller.go
type CouponController struct {
	couponsv1.UnimplementedCouponServiceServer
	useCoupon *usecoupon.UseCouponCommand
}

func (c *CouponController) UseCoupon(ctx context.Context, req *couponsv1.UseCouponRequest) (*couponsv1.UseCouponResponse, error) {
	err := c.useCoupon.Execute(ctx, domain.UserID(req.GetUserId()), domain.CouponID(req.GetCouponId()))
	if err != nil {
		return nil, err // domain errors are encoded by the interceptor
	}
	return &couponsv1.UseCouponResponse{}, nil
}
```

Assembly in `cmd/<app>/main.go`:

```go
srv := grpckit.NewServer(
	grpckit.WithLogger(logger),
	grpckit.WithErrorCode("coupon_not_found", codes.NotFound),
)
couponsv1.RegisterCouponServiceServer(srv, couponController)

if err := grpckit.Run(ctx, srv, cfg.GRPCAddr); err != nil {
	logger.Error("grpc server", "error", err)
}
```

`grpckit.Run` serves until the context is cancelled, then stops gracefully.

## Client side

`grpckit.Connect` + a generated client is the whole "universal client": serialization, request-id propagation, and error decoding are already handled. The gateway adapter only maps types:

```go
// features/order/adapters/grpc/permission_gateway.go
type PermissionGateway struct {
	client couponsv1.CouponServiceClient
}

func NewPermissionGateway(conn *grpc.ClientConn) *PermissionGateway {
	return &PermissionGateway{client: couponsv1.NewCouponServiceClient(conn)}
}
```

```go
// cmd/<app>/main.go
conn := grpckit.MustConnect(cfg.CouponsGRPCURL)
defer conn.Close()
gateway := grpcadapter.NewPermissionGateway(conn)
```

## Error mapping

The server interceptor guarantees clients see exactly two kinds of failures:

| Handler returns | Client receives |
|---|---|
| ddd domain error (`ddd.IsDomainError`) | status `FailedPrecondition` (or the `WithErrorCode` mapping) + `ErrorInfo` detail with the code |
| any other error, or a panic | status `Internal` with the message `"internal error"` — internals are logged server-side, never sent |

On the client, the interceptor restores domain errors as `*grpckit.RemoteDomainError` — `ddd.IsDomainError` keeps working **across the service boundary**, and callers branch on the code:

```go
err := gateway.Can(ctx, userID, couponID)

var remote *grpckit.RemoteDomainError
if errors.As(err, &remote) && remote.Code == "coupon_exhausted" {
	// expected business variant
}
```

## Request-id propagation

Every handler runs with a request id in context (`grpckit.RequestID(ctx)`) — taken from incoming `x-request-id` metadata or generated. The client interceptor forwards it on outgoing calls, so the id survives service hops. Seed it at the edge with `grpckit.WithRequestID(ctx, id)`.

## Testing

In-process round-trip over `bufconn`, no network — see [`grpckit/grpckit_test.go`](../../grpckit/grpckit_test.go) for the full pattern:

```go
lis := bufconn.Listen(1 << 20)
srv := grpckit.NewServer(grpckit.WithLogger(discard))
couponsv1.RegisterCouponServiceServer(srv, controller)
go srv.Serve(lis)

conn, _ := grpckit.Connect("passthrough:///bufnet",
	grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	}))
```

## Limitations (deliberate, for now)

- Unary RPCs only — the interceptor chain has no streaming variants yet; add them when a contract actually needs streams.
- Plaintext transport — services are assumed to talk inside a private network; TLS becomes a `Connect` option when a deployment needs it.

## Anti-patterns

| Anti-pattern | The right way |
|-------------|---------------|
| Business logic in the gRPC controller | Controllers map types and call use-cases |
| Mapping domain errors to statuses by hand in handlers | Return the domain error; the interceptor encodes it |
| Returning raw internal errors to clients | Automatic — anything non-domain is masked as `Internal` |
| Editing `contracts/gen/` by hand | Edit the proto, run `buf generate` |
| Breaking changes to a published `v1` proto | New version directory (`v2`) |
