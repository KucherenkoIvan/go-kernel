# requestid

The shared request-id context — one package, one context key, so an id set at any edge survives every hop: HTTP in → use-case → gRPC out → outbox → Kafka → the next service's handler.

- `Header` — the wire name (`x-request-id`) in HTTP headers, gRPC metadata, and Kafka headers
- `With` / `From` — put/read the id on a context
- `Ensure` — guarantee an id, generating a UUIDv7 if absent

```go
ctx = requestid.With(ctx, id)         // seed at an edge
log := logger.With("requestId", requestid.From(ctx))
```

Services rarely call this package directly — `httpapi`, `grpckit`, `kafka`, and `outbox` propagate the id automatically; it exists so they all agree.

Guide: [docs/guides/requestid.md](../docs/guides/requestid.md)
