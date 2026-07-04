# Application Layer

Orchestration of business scenarios. Imports from `domain/`, `shared/domain/`, `shared/ports/`. Does **NOT** import from `shared/infra/`.

## Ports

Interfaces defining the contracts for interacting with infrastructure. The application layer describes *what* it needs, and adapters decide *how* to implement it.

- **Repository** — saving and loading aggregates. Works with the aggregate, takes `Transaction` as the first argument after `ctx`. One repository per aggregate.
- **Reader** — reading and writing read-models. Takes `Transaction` as the first argument after `ctx`. Used in queries for reading and in projectors for updating projections.
- **Event Producer** — publishing domain events. Takes `Transaction` as the first argument after `ctx`, so that writing events is atomic with saving the aggregate (outbox pattern).
- **Gateway** — any other kind of port describing an external dependency, when it does not fit the standard kinds above.

```go
// --- Port (repository) ---
type CouponRepository interface {
	Save(ctx context.Context, tx ddd.Transaction, coupon *domain.Coupon) error
	GetByID(ctx context.Context, tx ddd.Transaction, id domain.CouponID) (*domain.Coupon, error) // (nil, nil) if not found
}

// --- Port (reader) ---
type CouponOverviewReader interface {
	GetByID(ctx context.Context, tx ddd.Transaction, id domain.CouponID) (*domain.CouponOverviewReadModel, error)
	Upsert(ctx context.Context, tx ddd.Transaction, data domain.CouponOverviewReadModel) error
}

// --- Port (event producer) ---
type CouponEventProducer interface {
	Publish(ctx context.Context, tx ddd.Transaction, events []domain.CouponEvent) error
}

// --- Port (gateway) ---
type PermissionGateway interface {
	Can(ctx context.Context, userID domain.UserID, action string, couponID domain.CouponID) error
}
```

There are ready-made ports that can be imported from the `ddd` package:
`TxManager`, `Transaction`, `IDGenerator`, `Clock`

Test implementations for them live there as well.

> **Why `tx` is a leading parameter:** repositories and producers take `Transaction` right after `ctx`. This forces you, at every call site, to explicitly answer the question "does this need a transaction?". If not — you pass `ddd.NoTransaction` consciously, instead of forgetting about the transaction by accident.

## Use-cases

The main code orchestrating business logic and connecting it to adapters. Each use-case consists of related commands and queries implementing a scenario.

Example use-case structure for a `coupon` feature:

```
usecases/
  managecoupons/
    create_coupon_command.go
    deactivate_coupon_command.go
    get_coupon_query.go
    get_coupons_list_query.go

  usecoupon/
    use_coupon_command.go
    coupon_used_query.go

  couponstats/
    get_coupon_stats_query.go
    coupon_stats_projector.go
```

We use the `CQRS` pattern — reads and writes are separated. Use-cases consist of four kinds of handlers:

| Kind | Purpose | Works with |
|-----|-----------|------------|
| **command** | Synchronous state-change request | aggregate |
| **query** | Synchronous read request | read-model (via a reader) |
| **policy** | Async event handling: business logic + command call | event -> command |
| **projector** | Async event handling: read-model update | event -> reader |


### Command

A synchronous state-change request. Loads the aggregate from the repository, calls business methods, saves, and publishes events. All work happens within one transaction.

```go
type UseCouponCommand struct {
	txManager  ddd.TxManager
	couponRepo ports.CouponRepository
	events     ports.CouponEventProducer
	permission ports.PermissionGateway
}

func NewUseCouponCommand(
	txManager ddd.TxManager,
	couponRepo ports.CouponRepository,
	events ports.CouponEventProducer,
	permission ports.PermissionGateway,
) *UseCouponCommand {
	return &UseCouponCommand{txManager, couponRepo, events, permission}
}

func (c *UseCouponCommand) Execute(ctx context.Context, userID domain.UserID, couponID domain.CouponID) error {
	if err := c.permission.Can(ctx, userID, "use-coupon", couponID); err != nil {
		return err
	}

	return c.txManager.WithinTx(ctx, func(tx ddd.Transaction) error {
		coupon, err := c.couponRepo.GetByID(ctx, tx, couponID)
		if err != nil {
			return err
		}
		if coupon == nil {
			return &domain.CouponNotFoundError{}
		}
		if err := coupon.Use(); err != nil {
			return err
		}
		if err := c.couponRepo.Save(ctx, tx, coupon); err != nil {
			return err
		}
		return c.events.Publish(ctx, tx, coupon.PopEvents())
	})
}
```

### Query

A synchronous read request. Reads a read-model through a reader, does not use aggregates. Does not change state.

```go
type GetCouponOverviewQuery struct {
	couponReader ports.CouponOverviewReader
}

func NewGetCouponOverviewQuery(couponReader ports.CouponOverviewReader) *GetCouponOverviewQuery {
	return &GetCouponOverviewQuery{couponReader}
}

func (q *GetCouponOverviewQuery) Execute(ctx context.Context, couponID domain.CouponID) (*domain.CouponOverviewReadModel, error) {
	return q.couponReader.GetByID(ctx, ddd.NoTransaction, couponID)
}
```

Yes, in simple cases this duplicates the read-model and the aggregate. But this way we explicitly control the data available for reading, and there is no "aggregate serialization" magic.

In the early stages both the reader port and the repository port work with the same database. But some read-models are more convenient to keep in separate tables or databases and synchronize through events. That is where the projector comes in.


### Projector

A reaction to an async event that updates a read-model. Receives events and writes denormalized data into a reader for fast reads.

```go
type CouponLogsProjector struct {
	logsReader ports.CouponLogsReader
}

func (p *CouponLogsProjector) Handle(ctx context.Context, events []domain.CouponUsedEvent) error {
	logs := make([]domain.CouponLogReadModel, 0, len(events))
	for _, e := range events {
		logs = append(logs, mapEventToReadModel(e))
	}
	return p.logsReader.Save(ctx, ddd.NoTransaction, logs)
}
```

**Important:** a projector works only with read-models. If you need reactive logic where an event triggers another command — use a policy.

> How events actually reach policies and projectors (in-process channel publisher now, transactional outbox later) is the adapter's concern — see the [events guide](../guides/events.md).


### Policy

A reaction to an async event with business logic. Receives an event, makes a decision, and calls a command. Connects different use-cases with each other.

Technically a command could be invoked straight from `adapters/kafka/event_handler.go`, but often there is business logic that must happen **before** the command call: event deduplication, filtering.

```go
// Domain event CouponUsedEvent -> Policy (filtering) -> SendCouponNotificationCommand
type CouponUsedNotificationPolicy struct {
	sendNotification *SendCouponNotificationCommand
}

func (p *CouponUsedNotificationPolicy) Handle(ctx context.Context, event domain.CouponUsedEvent) error {
	if event.Data.UsedCount < maxUsedCount {
		return nil
	}

	return p.sendNotification.Execute(ctx, SendCouponNotificationInput{
		CouponID: event.Data.CouponID,
		Code:     event.Data.Code,
	})
}
```
