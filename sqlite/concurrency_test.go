package sqlite_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/KucherenkoIvan/go-kernel/ddd"
	"github.com/KucherenkoIvan/go-kernel/sqlite"
)

// Two clients on one file, each running read-then-write transactions
// concurrently — the exact pattern that hits the deferred-transaction
// upgrade race (SQLITE_BUSY bypassing busy_timeout). BEGIN IMMEDIATE
// (_txlock=immediate in Open) makes it structurally impossible: this test
// fails without it.
func TestConcurrentWriters_TwoClients(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent.db")
	ctx := context.Background()

	open := func() *sqlite.Client {
		c, err := sqlite.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	a, b := open(), open()

	if _, err := a.Resolve(ddd.NoTransaction).ExecContext(ctx,
		`CREATE TABLE counter (id integer PRIMARY KEY CHECK (id = 1), n integer NOT NULL);
		 INSERT INTO counter (id, n) VALUES (1, 0)`); err != nil {
		t.Fatal(err)
	}

	const perClient = 25
	increment := func(c *sqlite.Client) error {
		return c.WithinTx(ctx, func(tx ddd.Transaction) error {
			var n int
			// read...
			if err := c.Resolve(tx).QueryRowContext(ctx, `SELECT n FROM counter WHERE id = 1`).Scan(&n); err != nil {
				return err
			}
			// ...then write — the upgrade pattern
			_, err := c.Resolve(tx).ExecContext(ctx, `UPDATE counter SET n = $1 WHERE id = 1`, n+1)
			return err
		})
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2*perClient)
	for _, client := range []*sqlite.Client{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perClient {
				if err := increment(client); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent write tx failed: %v", err)
	}

	var n int
	if err := a.Resolve(ddd.NoTransaction).QueryRowContext(ctx,
		`SELECT n FROM counter WHERE id = 1`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2*perClient {
		t.Fatalf("lost updates: n = %d, want %d", n, 2*perClient)
	}
}
