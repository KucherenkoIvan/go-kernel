# kafka

Contract-first Kafka messaging on franz-go (pure Go, no librdkafka/cgo). A contract pairs a topic with its protobuf type — producers and consumers import the same value, so topic and schema cannot drift apart.

- `Contract[T]` / `NewContract` — topic ↔ protobuf type pairing; `Encode`/`Decode`
- `Producer` — `Send` (typed) / `SendRaw` (used by the outbox relay), `Ping` for health, flushing `Close`
- `Consumer` + `Subscribe` — consumer group with per-contract handlers; at-least-once: offset commits only on handler success, errors retry the same message with backoff, undecodable payloads are logged and skipped
- Request-id propagation through message headers, both directions

```go
var OrderPlaced = kafka.NewContract[*eventsv1.OrderPlacedEvent]("example.order-placed.v1")

kafka.Subscribe(consumer, OrderPlaced, func(ctx context.Context, msg *eventsv1.OrderPlacedEvent, meta kafka.Meta) error {
	return createOrder.Execute(ctx, domain.OrderID(msg.GetOrderId()))
})
```

Business-critical events go through the [`outbox`](../outbox/), not direct `Send`. Integration tests self-provision Redpanda via testcontainers (`make integration`), or use `TEST_KAFKA_BROKERS` when set.

Guide: [docs/guides/kafka.md](../docs/guides/kafka.md) · Contracts: [contracts/](../contracts/)
