package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/KucherenkoIvan/go-kernel/ddd"
)

// Querier is what repositories and readers run their SQL against — the
// common query surface of the database and a transaction, satisfied by both
// *sql.DB and *sql.Tx.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

var (
	_ Querier       = (*sql.DB)(nil)
	_ Querier       = (*sql.Tx)(nil)
	_ ddd.TxManager = (*Client)(nil)
)

// liteTx is the ddd.Transaction token carrying the native transaction.
type liteTx struct {
	tx          *sql.Tx
	afterCommit []func(ctx context.Context)
}

func (*liteTx) DDDTransaction() {}

// AfterCommit implements ddd.AfterCommitter: fn runs only after a successful
// commit, and is dropped on rollback.
func (t *liteTx) AfterCommit(fn func(ctx context.Context)) {
	t.afterCommit = append(t.afterCommit, fn)
}

// WithinTx implements ddd.TxManager. Everything inside fn runs in one
// WRITE transaction (BEGIN IMMEDIATE — see Open): the write lock is taken
// up front, so concurrent writers queue on busy_timeout instead of racing
// the read→write upgrade. Reads that need no transaction use
// ddd.NoTransaction and never contend.
//
// Nested calls are not supported — pass the Transaction token down the call
// stack instead of starting a new one.
func (c *Client) WithinTx(ctx context.Context, fn func(tx ddd.Transaction) error) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	token := &liteTx{tx: tx}
	if err := fn(token); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: commit tx: %w", err)
	}
	for _, fn := range token.afterCommit {
		fn(ctx)
	}
	return nil
}

// Resolve turns a Transaction token into something to run SQL against:
// the native transaction for a token produced by WithinTx, or the database
// for ddd.NoTransaction.
//
// A token produced by another TxManager is a programming error and panics.
func (c *Client) Resolve(tx ddd.Transaction) Querier {
	switch t := tx.(type) {
	case *liteTx:
		return t.tx
	case nil:
		return c.db
	default:
		if tx == ddd.NoTransaction {
			return c.db
		}
		panic(fmt.Sprintf("sqlite: Resolve got a foreign transaction token %T — it was not produced by this TxManager", tx))
	}
}
