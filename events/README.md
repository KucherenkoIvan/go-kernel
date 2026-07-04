# events

In-process delivery of domain events to policies and projectors, commit-gated so no handler ever sees uncommitted state.

- `Producer` — the generic event-producer port shape
- `ChannelPublisher` — the in-process implementation: a buffered channel + dispatch loop, wired as an `app.Adapter`
- `Handler` — a subscription (policy or projector adapter), filtered by event name
- Commit-gating via `ddd.AfterCommit`: events published inside a transaction are delivered only after the commit; rollbacks produce nothing

```go
pub := events.NewChannelPublisher(events.WithLogger(logger))
pub.Subscribe(events.Handler{
	Name:   "coupon-stats-projector",
	Events: []string{"CouponUsedEvent"},
	Handle: func(ctx context.Context, e ddd.DomainEvent) error { ... },
})
// app.Adapter{Name: "events", Run: pub.Run, Close: pub.Close} — before the servers!
```

Semantics: at-most-once, in-memory, dies with the process — correct for same-service reactions. Durable cross-service delivery is the [`outbox`](../outbox/)'s job, behind the same port shape.

Guide: [docs/guides/events.md](../docs/guides/events.md)
