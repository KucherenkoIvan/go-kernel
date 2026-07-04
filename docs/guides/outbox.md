# Transactional Outbox

## Why

The problem: save data to the DB **and** send an event to Kafka atomically. Send the event first and crash before saving (or the other way around) — and the data diverges.

The solution: the event is written to the `outbox` table **in the same transaction** as the business data. A relay worker periodically reads the table, publishes to Kafka, and deletes the published rows.

```
Service (any replica)
  |
  | (1 transaction)
  ├── INSERT INTO orders ...
  └── outbox.Enqueue()  →  INSERT INTO outbox

Relay (one adapter in the same or a dedicated service)
  | (every 10s)
  ├── SELECT ... ORDER BY id LIMIT n     (serialized: advisory lock / parallel: SKIP LOCKED)
  ├── producer.SendRaw() per batch
  └── DELETE FROM outbox WHERE id = ANY(...)
        — all in one transaction: published-and-deleted, or neither
```

Delivery is **at-least-once** (a crash between publish and delete republishes the batch) — consumers are idempotent anyway (see [kafka.md](./kafka.md)). Ordering: `id` is UUIDv7, so `ORDER BY id` is creation order.

The kernel side lives in [`outbox`](../../outbox/).

## 1. Producer side: Enqueue

### 1.1 Create the table

The schema is the package's contract, the migration is the service's duty — append `outbox.MigrationSQL` to the service's migrations verbatim (id is generated app-side as UUIDv7; no `uuidv7()` SQL function needed).

### 1.2 Enqueue inside the business transaction

The feature's event-producer port gets an outbox adapter:

```go
// features/order/adapters/pg/event_publisher.go
type OrderEventPublisherOutbox struct {
	db *postgres.Client
}

func (p *OrderEventPublisherOutbox) Publish(ctx context.Context, tx ddd.Transaction, events []domain.OrderEvent) error {
	for _, e := range events {
		switch e := e.(type) {
		case domain.OrderPlacedEvent:
			err := outbox.Enqueue(ctx, p.db.Resolve(tx), kafkacontracts.ExampleOrderPlaced,
				&eventsexamplev1.OrderPlacedEvent{
					OrderId:    string(e.Data.OrderID),
					UserId:     string(e.Data.UserID),
					TotalCents: e.Data.TotalCents,
				},
				outbox.WithKey(string(e.Data.OrderID)))
			if err != nil {
				return err
			}
		}
	}
	return nil
}
```

The use-case stays untouched — `Save(tx) → Publish(tx, PopEvents())` in one `WithinTx`, exactly as the [application layer](../architecture/3-application-layer.md) prescribes. If `Save` fails, nothing is enqueued; if `Enqueue` fails, the save rolls back.

## 2. Relay side

```go
relay := outbox.NewRelay(db, producer, outbox.RelayConfig{
	// Interval: 10s, BatchSize: 100, Mode: serialized — the defaults
})

app.Adapter{Name: "outbox-relay", Run: relay.Run}
```

The relay drains the table on every tick (repeated batches until empty), inside one transaction per batch. A publish failure rolls the batch back — rows stay, the next tick retries.

### Modes

Overlap-protection between ticks and between replicas depends on `Mode`:

| | `serialized` (default) | `parallel` |
|---|---|---|
| Mechanism | `pg_try_advisory_xact_lock` — exactly one relayer per iteration | `FOR UPDATE SKIP LOCKED` — concurrent relayers take disjoint rows |
| Ordering | strict global `ORDER BY id` | per Kafka key only |
| When | consumers expect global order; one relayer's throughput covers the load; simple debugging | high throughput; ordering delegated to partition keys; multiple replicas scale publishing |

### Monitoring

`relay.PendingStats(ctx)` returns the queue depth and the age of the oldest pending row. A growing age means the relay is down or falling behind — the natural alert, and easy to expose as a health check or metric.

## 3. Direct Kafka vs Outbox vs ChannelPublisher

| | Outbox | Direct Kafka | events.ChannelPublisher |
|---|---|---|---|
| Guarantee | at-least-once, atomic with the business tx | at-most-once | at-most-once, in-memory |
| Scope | cross-service | cross-service | same service |
| Latency | seconds (polling) | immediate | immediate (post-commit) |
| Use for | business-critical events | telemetry, side-channels | same-service policies/projectors |

All three sit behind the same producer-port shape — switching is a composition-root change:

```go
var events ports.OrderEventProducer = pg.NewOrderEventPublisherOutbox(db)  // outbox
var events ports.OrderEventProducer = kafkaadapter.NewOrderEventPublisher(producer) // direct
```

## 4. Testing

The relay takes a `RawProducer` interface — tests use a fake and a real Postgres (gated on `TEST_POSTGRES_URL`), no broker needed. `relay.RelayOnce(ctx)` executes one drain manually:

```go
producer := &fakeProducer{}
relay := outbox.NewRelay(db, producer, outbox.RelayConfig{})
n, err := relay.RelayOnce(ctx)
```

See [`outbox/outbox_test.go`](../../outbox/outbox_test.go) for the full pattern, including the rollback-on-publish-failure case.

## Anti-patterns

| Anti-pattern | Why it is bad | The right way |
|-------------|-------------|---------------|
| Save and publish without a shared transaction | Data and events diverge on crashes | `Enqueue` inside the business `WithinTx` |
| Direct Kafka for business-critical events | At-most-once: a crash loses the event | Outbox |
| Two relays in `parallel` mode with order-sensitive consumers | Global order is gone | `serialized`, or key-based ordering |
| Deleting rows before the publish succeeds | Lost events | The relay's fetch→publish→delete is one tx — keep it that way |
| Alerting on queue *count* only | A steady small count can still mean "stuck oldest row" | Alert on `PendingStats` oldest age |
