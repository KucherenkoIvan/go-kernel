package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/KucherenkoIvan/go-kernel/ddd"
)

type foreignTx struct{}

func (foreignTx) DDDTransaction() {}

func TestResolve_NoTransaction(t *testing.T) {
	c := &Client{}
	if got := c.Resolve(ddd.NoTransaction); got != c.pool {
		t.Fatalf("NoTransaction should resolve to the pool")
	}
	if got := c.Resolve(nil); got != c.pool {
		t.Fatalf("nil should resolve to the pool")
	}
}

func TestResolve_ForeignTokenPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on a foreign transaction token")
		}
	}()
	(&Client{}).Resolve(foreignTx{})
}

// Integration tests run only when TEST_POSTGRES_URL is set, e.g.:
//
//	docker run --rm -d -p 5432:5432 -e POSTGRES_PASSWORD=test postgres:17-alpine
//	TEST_POSTGRES_URL=postgres://postgres:test@localhost:5432/postgres go test ./postgres/
func integrationClient(t *testing.T) *Client {
	t.Helper()
	url := os.Getenv("TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("TEST_POSTGRES_URL is not set")
	}
	c, err := Connect(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestWithinTx_CommitAndRollback(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	// a real (non-temporary) table — temp tables are per-connection, and the
	// pool hands out different connections across Resolve calls
	if _, err := c.Resolve(ddd.NoTransaction).Exec(ctx,
		`CREATE TABLE IF NOT EXISTS go_kernel_tx_test (id int PRIMARY KEY); TRUNCATE go_kernel_tx_test`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = c.Resolve(ddd.NoTransaction).Exec(ctx, `DROP TABLE IF EXISTS go_kernel_tx_test`)
	})

	count := func() int {
		var n int
		if err := c.Resolve(ddd.NoTransaction).QueryRow(ctx,
			`SELECT count(*) FROM go_kernel_tx_test`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// commit path
	err := c.WithinTx(ctx, func(tx ddd.Transaction) error {
		_, err := c.Resolve(tx).Exec(ctx, `INSERT INTO go_kernel_tx_test (id) VALUES (1)`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 1 {
		t.Fatalf("after commit: count = %d, want 1", n)
	}

	// rollback path
	boom := errors.New("boom")
	err = c.WithinTx(ctx, func(tx ddd.Transaction) error {
		if _, err := c.Resolve(tx).Exec(ctx, `INSERT INTO go_kernel_tx_test (id) VALUES (2)`); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("callback error not propagated: %v", err)
	}
	if n := count(); n != 1 {
		t.Fatalf("after rollback: count = %d, want 1 (insert must be rolled back)", n)
	}
}
