# ddd: Building Blocks

How to use the [`ddd`](../../ddd/README.md) package — the concrete API behind the [domain layer](../architecture/2-domain-layer.md) and [application layer](../architecture/3-application-layer.md) architecture docs. Package readme: [`ddd/README.md`](../../ddd/README.md).

## An aggregate, end to end

```go
package domain

type CouponID string

type couponState struct {
	id         CouponID
	code       string
	usageLimit int
	usedCount  int
}

type Coupon struct {
	ddd.EventRecorder[CouponEvent] // the event queue: PushEvent / PopEvents
	state couponState
}

func NewCoupon(id CouponID, code string, usageLimit int) *Coupon {
	c := &Coupon{state: couponState{id: id, code: code, usageLimit: usageLimit}}
	c.PushEvent(NewCouponCreatedEvent(CouponCreatedData{CouponID: id, Code: code}))
	return c
}

func (c *Coupon) ID() CouponID { return c.state.id }

func (c *Coupon) Use() error {
	if c.state.usedCount >= c.state.usageLimit {
		return &CouponExhaustedError{}
	}
	c.state.usedCount++
	c.PushEvent(NewCouponUsedEvent(CouponUsedData{CouponID: c.state.id, Code: c.state.code}))
	return nil
}
```

State is unexported; only aggregate methods mutate it. Persistence goes through explicit snapshots (`Snapshot()` / `RestoreCoupon()` — plain structs you write, no reflection).

## Events

```go
type CouponUsedData struct {
	CouponID CouponID
	Code     string
}

type CouponUsedEvent = ddd.Event[CouponUsedData] // ID (UUIDv7), Name, OccurredAt, Data

func NewCouponUsedEvent(data CouponUsedData) CouponUsedEvent {
	return ddd.NewEvent("CouponUsedEvent", data)
}

// deterministic variant for tests / replay:
event := ddd.NewEventAt("CouponUsedEvent", data, knownID, knownTime)
```

For a heterogeneous queue, `CouponEvent` is the `ddd.DomainEvent` envelope interface (or a narrower one); handlers type-assert back to the concrete event.

## Transactions

The application layer sees three things — the port, the token, and the sentinel:

```go
err := txManager.WithinTx(ctx, func(tx ddd.Transaction) error { // port
	coupon, err := repo.GetByID(ctx, tx, id)                     // token passed down
	if err != nil {
		return err
	}
	if err := coupon.Use(); err != nil {
		return err // rollback
	}
	if err := repo.Save(ctx, tx, coupon); err != nil {
		return err
	}
	return producer.Publish(ctx, tx, coupon.PopEvents())
})

model, err := reader.GetByID(ctx, ddd.NoTransaction, id) // sentinel: consciously no tx
```

`ddd.AfterCommit(ctx, tx, fn)` schedules work for after a successful commit (dropped on rollback; immediate with `NoTransaction`) — the mechanism behind commit-gated [event delivery](./events.md). Production implementations: [`postgres`](./postgres.md), [`sqlite`](./sqlite.md).

## Determinism ports

Domain code never calls `time.Now()` or generates UUIDs inline:

```go
type CreateCouponCommand struct {
	ids   ddd.IDGenerator // production: ddd.UUIDv7Generator{}
	clock ddd.Clock       // production: ddd.SystemClock{}
	// ...
}

func (c *CreateCouponCommand) Execute(ctx context.Context, code string) (domain.CouponID, error) {
	id := domain.CouponID(c.ids.NewID())
	coupon := domain.NewCoupon(id, code, 100)
	// ...
}
```

`ddd.NewEntityID()` is the package-level UUIDv7 generator (time-ordered — ids sort by creation time, which the outbox relies on).

## Domain errors

```go
type CouponExhaustedError struct{ ddd.DomainError } // embed the marker

func (e *CouponExhaustedError) Error() string { return "coupon_exhausted" }
```

`ddd.IsDomainError(err)` walks the chain and answers "expected business variant or failure?" — transports map domain errors to 4xx/rich statuses and mask everything else as 500/Internal. The code (`Error()`) travels across service boundaries intact (see [gRPC](./grpc.md)). Full decision tree: [domain layer](../architecture/2-domain-layer.md#domain-errors).

## Testing with dddtest

`ddd/dddtest` makes commands testable without a database:

```go
func TestUseCoupon(t *testing.T) {
	txm := &dddtest.FakeTxManager{}          // no real tx; honors AfterCommit on success
	clock := dddtest.FixedClock{Time: knownTime}
	ids := &dddtest.SequenceIDGenerator{}    // "id-1", "id-2", ...
	repo := &fakeCouponRepo{coupon: domain.RestoreCoupon(snap)}

	cmd := usecoupon.NewUseCouponCommand(txm, repo, producer, permission)
	if err := cmd.Execute(ctx, userID, couponID); err != nil {
		t.Fatal(err)
	}

	if repo.savedWith != txm.LastTransaction {
		t.Fatal("Save must run inside the transaction")
	}
}
```

## Anti-patterns

The domain and application layer tables apply verbatim: [domain](../architecture/2-domain-layer.md#anti-patterns), [application](../architecture/3-application-layer.md). The short version: no infra imports in domain, no invariant checks outside aggregate methods, no `time.Now()`/inline UUIDs in domain, business errors always carry the `ddd.DomainError` marker.
