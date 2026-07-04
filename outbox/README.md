# outbox

Transactional outbox: events are written to the `outbox` table in the same transaction as the business data, and a relay publishes them to Kafka — at-least-once, in creation order.

- `MigrationSQL` — the table schema (the package's contract; applying it is the service's duty)
- `Enqueue` — typed insert inside the business transaction, request-id preserved in headers
- `Relay` — fetch → publish → delete in one tx; `serialized` mode (advisory lock, strict global order) or `parallel` (`SKIP LOCKED`); wired as an `app.Adapter`
- `RelayOnce` — manual tick for tests; `PendingStats` — queue depth + oldest-row age for alerting

```go
err := txManager.WithinTx(ctx, func(tx ddd.Transaction) error {
	if err := orderRepo.Save(ctx, tx, order); err != nil {
		return err
	}
	return outbox.Enqueue(ctx, db.Resolve(tx), kafkacontracts.OrderPlaced,
		&eventsv1.OrderPlacedEvent{OrderId: id}, outbox.WithKey(id))
})
```

The relay publishes through a `RawProducer` interface — tests use a fake and real Postgres, no broker needed.

Guide: [docs/guides/outbox.md](../docs/guides/outbox.md) · Same-service alternative: [`events`](../events/)
