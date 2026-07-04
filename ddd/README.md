# ddd

Base building blocks for the domain and application layers. Zero dependencies — this is the only package the domain layer may import.

- `EventRecorder[E]` — embeddable event queue for aggregates (`PushEvent` / `PopEvents`)
- `Event[T]` + `NewEvent` / `NewEventAt` — domain events with a UUIDv7 id and typed payload
- `TxManager` / `Transaction` / `NoTransaction` — the transaction port and token
- `AfterCommit` — post-commit hooks on the token (what makes commit-gated event delivery possible)
- `Clock`, `IDGenerator`, `NewEntityID` (UUIDv7) — determinism ports for time and ids
- `DomainError` + `IsDomainError` — the marker separating expected business errors from failures

```go
type Coupon struct {
	ddd.EventRecorder[CouponEvent]
	state couponState
}

func (c *Coupon) Use() error {
	if c.state.usedCount >= c.state.usageLimit {
		return &CouponExhaustedError{} // embeds ddd.DomainError
	}
	c.state.usedCount++
	c.PushEvent(NewCouponUsedEvent(CouponUsedData{CouponID: c.state.id}))
	return nil
}
```

Test fakes live in [`dddtest`](dddtest/): `FakeTxManager` (records the token, honors `AfterCommit`), `FixedClock`, `SequenceIDGenerator`.

Guide: [docs/guides/ddd.md](../docs/guides/ddd.md) · Concepts: [domain layer](../docs/architecture/2-domain-layer.md), [application layer](../docs/architecture/3-application-layer.md)
