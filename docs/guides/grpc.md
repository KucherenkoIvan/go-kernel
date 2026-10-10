# gRPC

Synchronous communication between services. Contract-first: protos live in [`contracts/`](../../contracts/README.md), generated code is committed and imported from the module. The runtime side lives in [`grpckit`](../../grpckit/README.md).

## Contracts workflow

1. Add/edit a proto under `contracts/proto/grpc/<domain>/v1/` (Kafka event schemas go to `proto/events/` — see [kafka.md](./kafka.md)).
2. `buf lint && buf generate` (see [contracts/README.md](../../contracts/README.md) for one-time tooling setup).
3. Commit `proto/` and `gen/` together.

Consumers never run codegen — they import the generated package:

```go
import couponsv1 "github.com/KucherenkoIvan/go-kernel/contracts/gen/grpc/coupons/v1"
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

### Any listener (Unix sockets, named pipes)

`grpckit.Run` listens on TCP. For anything else — a local API on a Unix socket, a Windows named pipe, a socket handed over by the service manager — open the listener yourself and pass it to `grpckit.Serve`. The lifecycle is `Run`'s: serve until ctx is cancelled, then stop gracefully, giving in-flight RPCs 10s before their connections are closed; nil on cancel.

`Serve` owns both arguments. When it returns, the listener is closed and the server stopped, so a `*grpc.Server` goes to exactly one `Serve` call. If serving fails while ctx is live (the listener breaks, or someone stops the server), `Serve` hard-stops the server and returns the error at once.

Compose it as an adapter and open the listener *inside* `Run`, so a listen failure is that adapter's error and shuts the service down like any other ([app guide](./app.md)):

```go
app.Adapter{Name: "local-api", Run: func(ctx context.Context) error {
	lis, err := listenUnix(cfg.SocketPath)
	if err != nil {
		return err
	}
	return grpckit.Serve(ctx, srv, lis)
}},
```

```go
// listenUnix opens a Unix socket reachable only by the owner and group of its
// directory.
func listenUnix(path string) (net.Listener, error) {
	// Access control is the parent directory's mode, set before the socket
	// exists. Chmod on the socket after Listen is racy: clients can connect
	// (and sit in the backlog) before it lands.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o750); err != nil { // MkdirAll keeps an existing dir's mode
		return nil, err
	}

	// A crashed process leaves its socket file behind, and Listen then fails
	// with "address already in use". Remove it only if nothing answers —
	// otherwise another instance is serving.
	if fi, err := os.Lstat(path); err == nil && fi.Mode().Type() == fs.ModeSocket {
		if conn, err := net.Dial("unix", path); err == nil {
			_ = conn.Close()
			return nil, fmt.Errorf("%s: another server is listening", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	return net.Listen("unix", path)
}
```

The directory decides who may connect at all. Per-caller authorization (which local user is calling) belongs in a [`WithUnaryInterceptor`](#custom-interceptors) that reads the peer's credentials (`SO_PEERCRED` on Linux, `LOCAL_PEERCRED` on macOS).

Clients dial with gRPC's built-in `unix` scheme. In `unix://` form the path must be absolute (`unix:///run/myapp/api.sock`); `unix:` + path takes a relative one:

```go
conn := grpckit.MustConnect("unix://" + cfg.SocketPath) // cfg.SocketPath is absolute
```

Socket paths are limited to ~104 bytes on macOS (108 on Linux).

**Windows.** AF_UNIX sockets work on Windows 10 1803+, so the code above runs there too. Named pipes have no gRPC scheme: serve on a pipe listener (e.g. go-winio's `ListenPipe`) and dial with `grpckit.Connect("passthrough:///pipe", grpc.WithContextDialer(...))` around the pipe dialer (go-winio's `DialPipeContext`). Which of the two to use is the service's choice; the kernel depends on neither.

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

## Custom interceptors

Auth, rate limiting, metrics — appended via `WithUnaryInterceptor`, always running *inside* the standard chain, which means: panics recovered, request id already in context, and domain errors returned by an interceptor encoded like handler errors.

```go
srv := grpckit.NewServer(
	grpckit.WithLogger(logger),
	grpckit.WithUnaryInterceptor(AuthInterceptor(verifier)),
)
```

See the [middleware & auth guide](./middleware-auth.md) for the full pattern (Principal, exemptions, ordering semantics) on both transports.

## Request-id propagation

Every handler runs with a request id in context (`grpckit.RequestID(ctx)`) — taken from incoming `x-request-id` metadata or generated. The client interceptor forwards it on outgoing calls, so the id survives service hops — including HTTP→gRPC, since the id lives in the shared [`requestid`](../../requestid/README.md) package used by `httpapi` as well. Seed it at the edge with `grpckit.WithRequestID(ctx, id)`.

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
| `srv.Serve(lis)` + hand-written shutdown for a non-TCP listener | `grpckit.Serve(ctx, srv, lis)` — same bounded graceful stop as `Run` |
| `os.Chmod` on a Unix socket after `net.Listen` | Restrict the parent directory before listening |
| Opening the listener in `main` and passing it into an adapter | Open it inside the adapter's `Run`, so a listen failure is the adapter's error |
| Deleting a socket file unconditionally before `Listen` | Dial first; remove only if nothing answers |
| Breaking changes to a published `v1` proto | New version directory (`v2`) |
