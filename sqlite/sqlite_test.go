package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/KucherenkoIvan/go-kernel/ddd"
	"github.com/KucherenkoIvan/go-kernel/sqlite"
)

// No env gating, no containers — the database is embedded, so these tests
// always run.

func openMemory(t *testing.T) *sqlite.Client {
	t.Helper()
	c, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

type foreignTx struct{}

func (foreignTx) DDDTransaction() {}

func TestResolve(t *testing.T) {
	c := openMemory(t)
	if got := c.Resolve(ddd.NoTransaction); got != c.DB() {
		t.Fatal("NoTransaction should resolve to the database")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on a foreign transaction token")
		}
	}()
	c.Resolve(foreignTx{})
}

func TestWithinTx_CommitRollbackAndHooks(t *testing.T) {
	c := openMemory(t)
	ctx := context.Background()

	if _, err := c.Resolve(ddd.NoTransaction).ExecContext(ctx,
		`CREATE TABLE tx_test (id integer PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		var n int
		if err := c.Resolve(ddd.NoTransaction).QueryRowContext(ctx,
			`SELECT count(*) FROM tx_test`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// commit path + after-commit hook
	hookRan := false
	err := c.WithinTx(ctx, func(tx ddd.Transaction) error {
		ddd.AfterCommit(ctx, tx, func(context.Context) { hookRan = true })
		_, err := c.Resolve(tx).ExecContext(ctx, `INSERT INTO tx_test (id) VALUES (1)`)
		if hookRan {
			t.Fatal("hook must not run before commit")
		}
		return err
	})
	if err != nil || count() != 1 || !hookRan {
		t.Fatalf("commit path: err=%v count=%d hook=%v", err, count(), hookRan)
	}

	// rollback path: insert undone, hook dropped
	hookRan = false
	boom := errors.New("boom")
	err = c.WithinTx(ctx, func(tx ddd.Transaction) error {
		ddd.AfterCommit(ctx, tx, func(context.Context) { hookRan = true })
		if _, err := c.Resolve(tx).ExecContext(ctx, `INSERT INTO tx_test (id) VALUES (2)`); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) || count() != 1 || hookRan {
		t.Fatalf("rollback path: err=%v count=%d hook=%v", err, count(), hookRan)
	}
}

func TestMigrate(t *testing.T) {
	// a real file — migrations must survive reopening
	path := filepath.Join(t.TempDir(), "app.db")

	fsys := fstest.MapFS{
		"0001_create.sql": &fstest.MapFile{Data: []byte(
			`CREATE TABLE items (id text PRIMARY KEY, name text NOT NULL);
			 CREATE INDEX items_name_idx ON items (name);`)},
		"0002_seed.sql": &fstest.MapFile{Data: []byte(
			`INSERT INTO items (id, name) VALUES ('i-1', 'first');`)},
	}

	c, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Migrate(context.Background(), fsys); err != nil {
		t.Fatal(err)
	}
	_ = c.Close()

	// reopen: nothing reapplied, data intact
	c = sqlite.MustOpen(path)
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Migrate(context.Background(), fsys); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := c.Resolve(ddd.NoTransaction).QueryRowContext(context.Background(),
		`SELECT count(*) FROM items`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("seed applied %d times, want 1", n)
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	c := openMemory(t)
	ctx := context.Background()

	if _, err := c.Resolve(ddd.NoTransaction).ExecContext(ctx, `
		CREATE TABLE parents (id text PRIMARY KEY);
		CREATE TABLE children (id text PRIMARY KEY, parent_id text NOT NULL REFERENCES parents(id));
	`); err != nil {
		t.Fatal(err)
	}

	_, err := c.Resolve(ddd.NoTransaction).ExecContext(ctx,
		`INSERT INTO children (id, parent_id) VALUES ('c-1', 'nope')`)
	if err == nil {
		t.Fatal("foreign keys must be enforced by the default pragmas")
	}
}
