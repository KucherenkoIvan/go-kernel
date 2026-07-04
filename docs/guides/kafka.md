# Working with Kafka

Asynchronous communication between services. Contract-first: event schemas are protobuf files in [`contracts/`](../../contracts/README.md), and a contract value pairs the topic with its message type. The runtime side lives in [`kafka`](../../kafka/README.md) (franz-go under the hood — pure Go, no librdkafka/cgo).

## 1. Describe the event contract

Create the schema in `contracts/proto/events/<domain>/v1/`:

```protobuf
// contracts/proto/events/example/v1/order.proto
syntax = "proto3";

package events.example.v1;

message OrderPlacedEvent {
  string order_id = 1;
  string user_id = 2;
  int64 total_cents = 3;
}
```

Generate (`cd contracts && buf generate`), then declare the contract — the pairing of topic and type that producers and consumers share:

```go
// contracts/kafkacontracts/example.go
var ExampleOrderPlaced = kafka.NewContract[*eventsexamplev1.OrderPlacedEvent]("example.order-placed.v1")
```

Topic naming: `<domain>.<event>.<version>`. Events are facts, named in past tense.

## 2. Consume

The Kafka handler is an adapter (`features/<feature>/adapters/kafka/`): decode is already done by the contract, so it maps the message and calls a policy or command.

```go
// cmd/<app>/main.go
consumer := kafka.NewConsumer(kafka.ConsumerConfig{
	Brokers: cfg.KafkaBrokers,
	GroupID: "myservice", // offsets are tracked per group
	Logger:  logger,
})
kafka.Subscribe(consumer, kafkacontracts.ExampleOrderPlaced, orderHandler.Handle)

app.Adapter{Name: "kafka-consumer", Run: consumer.Run}
```

```go
// features/order/adapters/kafka/order_placed_handler.go
func (h *OrderPlacedHandler) Handle(ctx context.Context, msg *eventsexamplev1.OrderPlacedEvent, meta kafka.Meta) error {
	return h.createOrder.Execute(ctx, domain.OrderID(msg.GetOrderId()), domain.UserID(msg.GetUserId()))
}
```

### Offset semantics (at-least-once)

The offset is committed **only after the handler returns nil**. Returning an error means "redeliver": the consumer retries the same message with exponential backoff (1s → 30s cap) and does not advance past it — so handlers MUST be idempotent, and a poison message blocks its partition (by design; a DLQ is future work). Two consequences:

- Do not swallow errors if you want the retry — return them.
- A payload that cannot be *decoded* is logged and skipped: retrying cannot fix a bad payload.

### Starting offset for a new consumer group

The default is `earliest` — it applies only when the group has no committed offset (first start, expired retention, renamed group, new partitions). Safe default: accumulated events are not lost; idempotent handlers reprocess them. For topics where history is noise (metrics, ephemeral fan-out), set `FromLatest: true` explicitly.

## 3. Produce

### Via outbox (recommended)

Business-critical events must be atomic with the state change — use the [transactional outbox](./outbox.md).

### Direct

For telemetry and side-channel notifications, where losing a message on a crash is acceptable:

```go
producer, err := kafka.NewProducer(kafka.ProducerConfig{Brokers: cfg.KafkaBrokers})

err = kafka.Send(ctx, producer, kafkacontracts.ExampleOrderPlaced,
	&eventsexamplev1.OrderPlacedEvent{OrderId: string(order.ID())},
	kafka.WithKey(string(order.ID()))) // same key → same partition → ordered
```

## Health check & lifecycle

The producer registers on the service's checker like every dependency (see [service runtime](./service-runtime.md)), and closes through the app runner so buffered messages flush on shutdown:

```go
health.Check{Name: "kafka", Check: producer.Ping}
app.Adapter{Name: "kafka-producer", Close: producer.Close}
```

The consumer needs no check of its own — it is a `Run` adapter; if it dies, `app.Run` shuts the service down, which the orchestrator sees directly.

## Request-id propagation

`kafka.Send` (and `outbox.Enqueue`) writes the context's request id into message headers; the consumer restores it into the handler's context. One id traces a request through HTTP → use-case → outbox → Kafka → the next service's handler.

## Testing

Integration tests are gated on `TEST_KAFKA_BROKERS` and run against a real broker — Redpanda in Docker is the light option (see `kafka/kafka_integration_test.go` for the one-liner). Code that *publishes* is usually tested with a fake `Producer`-port implementation instead; the broker round-trip is the kernel's job to verify, not every feature's.

## Anti-patterns

| Anti-pattern | The right way |
|-------------|---------------|
| Business-critical events via direct `Send` | Outbox — atomic with the business transaction |
| Handler swallows the error and returns nil | Return the error — that is the retry mechanism |
| Non-idempotent handlers | At-least-once delivery makes duplicates a fact of life |
| Publishing domain objects | Map to the protobuf contract in the adapter |
| Topic strings inline at call sites | One contract value in `contracts/kafkacontracts/` |
