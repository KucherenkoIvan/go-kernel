# PostgreSQL & Transactions

How services connect to PostgreSQL, run transactions, and structure repositories. The kernel side lives in [`postgres`](../../postgres/); everything service-specific lives in the service's `internal/shared/infra/postgres/`.

## Layout

All DB wiring, schema, and migrations live in `shared/infra` — **not** in the service repo root:

```
internal/
  shared/
    infra/
      postgres/
        postgres.go          # connect from app config, health check
        migrations/          # schema source of truth, applied on deploy
          0001_coupons.sql
          0002_outbox.sql
```

Feature adapters (`features/<feature>/adapters/pg/`) contain only repositories and readers — they receive the `*postgres.Client` through the composition root and never touch connection or migration concerns.

## Why TxManager

1. **Atomicity** — save + publish event in one transaction. Without a transaction, data may be saved while the event is not sent (or vice versa).
2. **Abstraction** — the application layer knows nothing about the concrete driver. It works with an abstract `Transaction` token.
3. **Testability** — `TxManager` is a port; in tests it is replaced with `dddtest.FakeTxManager`.

## API

```go
import (
	"github.com/KucherenkoIvan/go-kernel/ddd"
	"github.com/KucherenkoIvan/go-kernel/postgres"
)
```

### `ddd.TxManager`

The port. A single method:

```go
txManager.WithinTx(ctx, func(tx ddd.Transaction) error { ... })
```

Everything inside the callback runs in one DB transaction. If the callback returns an error — the transaction is rolled back.

### `ddd.Transaction`

A lightweight token. It does not carry the native DB transaction — it serves as a key for obtaining one in adapters via `client.Resolve(tx)`.

### `ddd.NoTransaction`

A constant for cases when a transaction is not needed.

### `postgres.Client`

The production implementation: a pgx v5 pool that **is** the TxManager — no separate class needed.

```go
db := postgres.MustConnect(ctx, cfg.DatabaseURL)
defer db.Close()

var txManager ddd.TxManager = db
```

## Why `tx` is a leading parameter

Repositories and producers take `Transaction` right after `ctx`. This forces you, at every call site, to explicitly answer the question: "does this need a transaction?". If not — you pass `ddd.NoTransaction` consciously, instead of forgetting about the transaction by accident.

```go
type OrderRepository interface {
	Save(ctx context.Context, tx ddd.Transaction, order *domain.Order) error
	GetByID(ctx context.Context, tx ddd.Transaction, id domain.OrderID) (*domain.Order, error)
}

type OrderEventProducer interface {
	Publish(ctx context.Context, tx ddd.Transaction, events []domain.OrderEvent) error
}
```

## Usage patterns

### Reading without a transaction

```go
order, err := r.orderRepo.GetPendingByDealID(ctx, ddd.NoTransaction, input.DealID)
```

### Atomic write (save + event via outbox)

The main use-case: persist state and publish an event atomically.

```go
err := c.txManager.WithinTx(ctx, func(tx ddd.Transaction) error {
	if err := c.orderRepo.Save(ctx, tx, order); err != nil {
		return err
	}
	return c.events.Publish(ctx, tx, order.PopEvents()) // writes into the outbox table
})
// If Save fails — Publish never runs
// If Publish fails — Save is rolled back
```

## Implementing a repository

`client.Resolve(tx)` returns a `postgres.Querier` — the native transaction for a token produced by `WithinTx`, or the pool for `ddd.NoTransaction`. Both satisfy the same interface, so repository code does not branch.

Row scanning uses [scany](https://github.com/georgysavva/scany) (`pgxscan`) — raw SQL stays, struct-scanning boilerplate goes:

```go
// features/coupon/adapters/pg/coupon_repository.go
type CouponRepository struct {
	db *postgres.Client
}

type couponRow struct {
	ID         string `db:"id"`
	Code       string `db:"code"`
	UsageLimit int    `db:"usage_limit"`
	UsedCount  int    `db:"used_count"`
}

func (r *CouponRepository) GetByID(ctx context.Context, tx ddd.Transaction, id domain.CouponID) (*domain.Coupon, error) {
	var row couponRow
	err := pgxscan.Get(ctx, r.db.Resolve(tx), &row,
		`SELECT id, code, usage_limit, used_count FROM coupons WHERE id = $1`, string(id))
	if pgxscan.NotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("selecting coupon: %w", err)
	}
	return rowToCoupon(row), nil
}
```

> [sqlc](https://sqlc.dev) can be layered on later per-repository without touching the kernel: `postgres.Querier` is intentionally shaped to match sqlc's `DBTX`, so generated queries plug straight into `Resolve(tx)`.

## Migrations

Plain SQL files in `internal/shared/infra/postgres/migrations/`, numbered, embedded into the binary and applied by the built-in runner:

```go
//go:embed migrations/*.sql
var migrationsFS embed.FS

sub, _ := fs.Sub(migrationsFS, "migrations")
if err := db.Migrate(ctx, sub); err != nil { ... }
```

`Migrate` applies `*.sql` files in lexicographic order, once each (`schema_migrations` bookkeeping), one transaction per file, serialized across replicas with an advisory lock. Up-only by design; [goose](https://github.com/pressly/goose) remains an option if a project outgrows it. Rules that hold regardless of runner:

- Migrations are the schema source of truth — no schema mutation from application code.
- A migration that adds a column adds its indexes in the same file.
- Down migrations are a non-goal — roll forward.

## Testing

`dddtest.FakeTxManager` creates no real transactions — it just invokes the callback and records the token:

```go
txManager := &dddtest.FakeTxManager{}
command := usecoupon.NewUseCouponCommand(txManager, couponRepo, events, permission)

_ = command.Execute(ctx, userID, couponID)

// assert that Save was called inside the transaction:
// couponRepo received txManager.LastTransaction
```

## Anti-patterns

| Anti-pattern | Why it is bad | The right way |
|-------------|-------------|---------------|
| Save and publish without a transaction | Data and events drift apart | `WithinTx(ctx, func(tx) { Save(tx); Publish(tx) })` |
| Long business logic inside a transaction | Holds locks, blocks other requests | Compute everything before the transaction, only writes inside |
| Getting the native tx bypassing the token | Breaks the abstraction, untestable | Use `client.Resolve(tx)` |
| Nested `WithinTx` calls | Undefined rollback behavior | Pass `tx` down the call stack |
| `ddd.NoTransaction` for writes | No atomicity | Use `WithinTx` for all write operations |
| Migrations / connection setup in feature adapters | DB wiring leaks out of infra | Everything DB-global lives in `internal/shared/infra/postgres/` |
