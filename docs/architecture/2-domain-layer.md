# Domain Layer

Pure business logic with no dependencies on frameworks or infrastructure. Imports only from `shared/domain/`, the standard library, and the `ddd` package of this module.

### Allowed dependencies in domain

| Package | Why |
|-------|-------|
| `github.com/KucherenkoIvan/go-kernel/ddd` | Base building blocks: `EventRecorder`, `NewEvent`, `Clock`, etc. *(package to be published from this module)* |
| `time` (stdlib) | Working with dates/time. `time.Time` is immutable by design. Never call `time.Now()` in domain — accept time as an argument or use the `Clock` port for testability |

```
domain/
  coupon_aggregate.go        # root object with business logic
  line_item_entity.go        # child object of an aggregate
  discount_vo.go             # value object
  events.go                  # domain events
  read_models.go             # read models
  errors.go                  # business errors
```


## Aggregate

> In classic DDD an aggregate is a narrower concept: a transactional consistency boundary. We use the term more broadly: **an aggregate is any root entity that the application layer works with directly** (creates, loads, saves, publishes events from). If an entity is only needed as part of a larger object — it is modeled as an entity.

An aggregate contains business rules, mutates through methods, and generates domain events. If an aggregate grows too large — part of the logic is moved into a child entity.

The `ddd` package provides an embeddable `EventRecorder[E]`:

- `PushEvent(event E)` — add a domain event to the internal queue (called only from aggregate methods)
- `PopEvents() []E` — take and clear accumulated events (called in the application layer after persist)

State lives in unexported fields — there is no `protected` in Go, so encapsulation is done by the package boundary: only code in the `domain` package can touch the state, and objects are created only through exported factory functions. `Snapshot()` returns a plain struct copy of the state for persistence; `RestoreCoupon()` re-creates an aggregate from DB data.

Identifiers are simple types: `string` (UUIDv7) or `int64`. For every aggregate/entity a **defined type** is declared — in Go this is stronger than a readability alias: passing a `UserID` where an `OrderID` is expected does not compile. UUIDv7 generation uses `ddd.NewEntityID()`.

```go
type CouponID string

type couponState struct {
	id         CouponID
	code       string
	discount   Discount
	usageLimit int
	usedCount  int
}

type Coupon struct {
	ddd.EventRecorder[CouponEvent]
	state couponState
}

func NewCoupon(id CouponID, code string, discount Discount, usageLimit int) *Coupon {
	c := &Coupon{state: couponState{id: id, code: code, discount: discount, usageLimit: usageLimit}}
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

```go
// Persistence: serialize into a plain snapshot before writing to the DB
snap := coupon.Snapshot()
// CouponSnapshot{ID: "...", Code: "SUMMER10", Discount: ..., UsageLimit: 100, UsedCount: 1}

// Restoration: from a snapshot (out of the DB) back into an aggregate
coupon := domain.RestoreCoupon(domain.CouponSnapshot{
	ID:         row.ID,
	Code:       row.Code,
	Discount:   domain.RestoreDiscount(row.DiscountPercent, row.DiscountMaxAmount),
	UsageLimit: row.UsageLimit,
	UsedCount:  row.UsedCount,
})
```

> **Naming:** the file is `coupon_aggregate.go`, but the type is `Coupon`, not `CouponAggregate` — the `domain` package already provides the context and Go style avoids stutter (`domain.Coupon`).

### Invariants

An invariant is a condition the aggregate guarantees **always**, no matter who calls it or how. Aggregate methods are the only place where invariants are checked. The application layer must not duplicate these checks.

```go
func (c *Coupon) Use() error {
	// invariant: a coupon cannot be used past its limit
	if c.state.usedCount >= c.state.usageLimit {
		return &CouponExhaustedError{}
	}
	// invariant: an inactive coupon cannot be used
	if !c.state.isActive {
		return &CouponInactiveError{}
	}

	c.state.usedCount++
	c.PushEvent(NewCouponUsedEvent(CouponUsedData{CouponID: c.state.id, Code: c.state.code}))
	return nil
}
```

### Events and the application layer

After the aggregate is saved, the application layer takes the accumulated events and publishes them:

```go
// use-case / command handler
if err := c.couponRepo.Save(ctx, tx, coupon); err != nil { // 1. persist
	return err
}
events := coupon.PopEvents()                               // 2. take the events
return c.eventProducer.Publish(ctx, tx, events)            // 3. publish
```

`PopEvents()` clears the internal queue — a repeated call returns an empty slice.

### Rules

1. An aggregate cannot reference another aggregate directly — only by id. Communication goes through events or the application layer.
2. Repositories work only with aggregates. An entity has no repository of its own — it is saved and loaded only as part of its parent aggregate. Except for rare cases where DB access needs optimization.

**When an aggregate grows, part of the logic can be moved into child entities and value objects.**

## Entity

An entity is a child object inside an aggregate. Used when the aggregate becomes too large and needs decomposition: part of the state and logic is moved into a separate object with its own id, but **not accessible from the application layer directly** — only through the parent aggregate.

Entities follow the same shape as aggregates minus the `EventRecorder`: unexported state, exported factory, mutations through methods.

```go
type LineItemID string

type lineItemState struct {
	id        LineItemID
	productID ProductID
	quantity  int
	price     Money
}

type LineItem struct {
	state lineItemState
}

func NewLineItem(id LineItemID, productID ProductID, quantity int, price Money) *LineItem {
	return &LineItem{state: lineItemState{id: id, productID: productID, quantity: quantity, price: price}}
}

func (li *LineItem) IncreaseQuantity(by int) error {
	if by <= 0 {
		return &InvalidQuantityError{}
	}
	li.state.quantity += by
	return nil
}

func (li *LineItem) Total() Money {
	return li.state.price.Multiply(li.state.quantity)
}
```

```go
// The aggregate holds child entities in its state
type orderState struct {
	id     OrderID
	items  []*LineItem // child entities
	status OrderStatus
}
```


## Value Object

An immutable object without identity. Two VOs with the same fields are equal. Implemented as a **struct with unexported fields + constructor and methods**, or a defined type over a primitive.

Mandatory:
- `NewXxx()` — validation + creation. Returns an error on invalid data

The type may also have:
- **immutable update** methods — value receiver, return an updated copy
- **getters** — state derived from the VO's fields

```go
type Discount struct {
	percent   int
	maxAmount int64 // 0 = no limit
}

func NewDiscount(percent int, maxAmount int64) (Discount, error) {
	if percent < 1 || percent > 100 {
		return Discount{}, &InvalidDiscountError{}
	}
	return Discount{percent: percent, maxAmount: maxAmount}, nil
}

// immutable update — value receiver mutates a copy, the original is untouched
func (d Discount) WithMaxAmount(maxAmount int64) Discount {
	d.maxAmount = maxAmount
	return d
}

// getter — derived state
func (d Discount) IsUnlimited() bool {
	return d.maxAmount == 0
}

// comparison — structs with comparable fields support == directly
// two VOs are equal if all their fields are equal: a == b
```

A VO wrapping a primitive (single-value VO) is a defined type:

```go
type Email string

func NewEmail(raw string) (Email, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if !emailRe.MatchString(normalized) {
		return "", &InvalidEmailError{}
	}
	return Email(normalized), nil
}
```

**When you need one:** a set of fields that always travel together; validation on creation; a domain concept instead of a primitive. If it is just a string with no rules — keep the primitive.

> Immutability is by convention: VOs are passed **by value** and keep their fields unexported, so nothing outside the package can mutate them.


## Domain Events

A fact that happened in the domain. Immutable. Created via `ddd.NewEvent`. Contains `ID` (UUIDv7), `Name`, `OccurredAt` (`time.Time`), and `Data`.

Events are generated in domain via `c.PushEvent(NewXxxEvent(...))` and handled in the application layer via `aggregate.PopEvents()`.

```go
type CouponUsedData struct {
	CouponID CouponID
	Code     string
}

type CouponUsedEvent = ddd.Event[CouponUsedData]

// Variant 1: standard — ID and OccurredAt are generated automatically
func NewCouponUsedEvent(data CouponUsedData) CouponUsedEvent {
	return ddd.NewEvent("CouponUsedEvent", data)
}
// event.ID         → UUIDv7 (generated)
// event.OccurredAt → time.Now() at the moment of the call

// Variant 2: with overrides — useful in tests or when replaying events
event := ddd.NewEventAt("CouponUsedEvent", data, knownID, knownTime)
```

## Read Model

A flat data structure for reading. Built as a DB query in a `reader` or asynchronously via a `projector` (see [application layer](./3-application-layer.md)).

A read model is a plain struct with exported fields. The `ReadModel` suffix states the intent: this structure is only read, never mutated.

```go
type CouponOverviewReadModel struct {
	ID              string
	Code            string
	DiscountPercent int
	UsedCount       int
	IsActive        bool
}
```


## Domain Errors

Typed errors returned when invariants are violated. They are values, not panics — a domain error is part of the method's contract.

```go
// Simple error
type CouponExhaustedError struct{ ddd.DomainError }

func (e *CouponExhaustedError) Error() string { return "coupon_exhausted" }

// Error with data
type DuplicateCodeError struct {
	ddd.DomainError
	Code string
}

func (e *DuplicateCodeError) Error() string { return "duplicate_code" }

// With a cause (wrapping a low-level error)
type PaymentFailedError struct {
	ddd.DomainError
	cause error
}

func (e *PaymentFailedError) Error() string { return "payment_failed" }
func (e *PaymentFailedError) Unwrap() error { return e.cause }
```

Callers distinguish errors with `errors.As` / `errors.Is`. Go has no union types — the set of expected errors of a use-case is part of its documented contract:

```go
// Use() returns:
//   *CouponExhaustedError — the usage limit is reached
//   *CouponInactiveError  — the coupon is deactivated
func (c *Coupon) Use() error { ... }
```

`ddd.DomainError` is an embeddable marker that separates expected business errors from everything else (transport layers map domain errors to response codes; all other errors become 500 and go to error tracking).

## Anti-patterns

| Anti-pattern | The right way |
|-------------|---------------|
| Importing frameworks / ORM / HTTP in domain | Domain imports only stdlib, `ddd`, and `shared/domain/` |
| Business logic in a use-case instead of the aggregate | Mutations and rules live in aggregate/entity methods |
| Huge aggregate (>300 lines) | Extract child entities or VOs |
| Business errors as plain `errors.New` / `fmt.Errorf` | Typed error with the `ddd.DomainError` marker |
| Generating UUIDs directly in domain | Generate ids in the application layer (`IDGenerator`) and pass into `NewXxx()` |
| `id string` without a defined type | Use `type OrderID string` — real type safety, readable signatures |
| `time.Now()` in domain | Accept time as an argument; `Clock` port for testability |
| A repository for a child entity | Repositories work only with aggregates; an entity is persisted as part of its aggregate |
| `panic` for business situations | Domain errors are returned values; `panic` only for impossible states (bugs) |
