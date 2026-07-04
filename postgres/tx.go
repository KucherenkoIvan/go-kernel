package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/KucherenkoIvan/go-kernel/ddd"
)

// Querier is what repositories and readers run their SQL against — the
// common query surface of a pool and a transaction. It is intentionally
// shaped to match scany's pgxscan.Querier and sqlc's DBTX, so both plug in
// without adapters.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

var (
	_ Querier       = (*pgxpool.Pool)(nil)
	_ Querier       = (pgx.Tx)(nil)
	_ ddd.TxManager = (*Client)(nil)
)

// pgTx is the ddd.Transaction token carrying the native pgx transaction.
type pgTx struct {
	tx pgx.Tx
}

func (*pgTx) DDDTransaction() {}

// WithinTx implements ddd.TxManager. Everything inside fn runs in one DB
// transaction; if fn returns an error, the transaction is rolled back.
//
// Nested calls are not supported — pass the Transaction token down the call
// stack instead of starting a new one.
func (c *Client) WithinTx(ctx context.Context, fn func(tx ddd.Transaction) error) error {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	if err := fn(&pgTx{tx: tx}); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit tx: %w", err)
	}
	return nil
}

// Resolve turns a Transaction token into something to run SQL against:
// the native transaction for a token produced by WithinTx, or the pool for
// ddd.NoTransaction.
//
// A token produced by another TxManager is a programming error and panics.
func (c *Client) Resolve(tx ddd.Transaction) Querier {
	switch t := tx.(type) {
	case *pgTx:
		return t.tx
	case nil:
		return c.pool
	default:
		if tx == ddd.NoTransaction {
			return c.pool
		}
		panic(fmt.Sprintf("postgres: Resolve got a foreign transaction token %T — it was not produced by this TxManager", tx))
	}
}
