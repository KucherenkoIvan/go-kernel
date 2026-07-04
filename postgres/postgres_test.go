package postgres

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/KucherenkoIvan/go-kernel/ddd"
	"github.com/KucherenkoIvan/go-kernel/internal/testinfra"
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

// Integration tests self-provision postgres via testcontainers when
// INTEGRATION=1 (`make integration`), use TEST_POSTGRES_URL when set, and
// skip otherwise.
func integrationClient(t *testing.T) *Client {
	t.Helper()
	c, err := Connect(context.Background(), testinfra.PostgresURL(t))
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

func TestWithinTx_AfterCommitHooks(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	ran := false
	err := c.WithinTx(ctx, func(tx ddd.Transaction) error {
		ddd.AfterCommit(ctx, tx, func(context.Context) { ran = true })
		if ran {
			t.Fatal("hook must not run before commit")
		}
		return nil
	})
	if err != nil || !ran {
		t.Fatalf("hook must run after commit: err=%v ran=%v", err, ran)
	}

	ran = false
	_ = c.WithinTx(ctx, func(tx ddd.Transaction) error {
		ddd.AfterCommit(ctx, tx, func(context.Context) { ran = true })
		return errors.New("rollback")
	})
	if ran {
		t.Fatal("hook must not run after rollback")
	}
}

func TestMigrate(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	t.Cleanup(func() {
		_, _ = c.Resolve(ddd.NoTransaction).Exec(ctx,
			`DROP TABLE IF EXISTS go_kernel_migrate_test; DELETE FROM schema_migrations WHERE version LIKE '00%_test_%.sql'`)
	})

	fsys := fstest.MapFS{
		"0001_test_create.sql": &fstest.MapFile{Data: []byte(
			`CREATE TABLE go_kernel_migrate_test (id int PRIMARY KEY);
			 CREATE INDEX go_kernel_migrate_test_idx ON go_kernel_migrate_test (id);`)},
		"0002_test_seed.sql": &fstest.MapFile{Data: []byte(
			`INSERT INTO go_kernel_migrate_test (id) VALUES (1)`)},
	}

	if err := c.Migrate(ctx, fsys); err != nil {
		t.Fatal(err)
	}
	// idempotent: second run applies nothing
	if err := c.Migrate(ctx, fsys); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := c.Resolve(ddd.NoTransaction).QueryRow(ctx,
		`SELECT count(*) FROM go_kernel_migrate_test`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("seed applied %d times, want 1", n)
	}
}
