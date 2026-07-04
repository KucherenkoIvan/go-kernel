package postgres

import (
	"context"
	"fmt"
	"hash/fnv"
	"io/fs"
	"sort"
)

// migrateLockID serializes concurrent migrators (e.g. two replicas starting
// at once) via a session-level advisory lock.
var migrateLockID = func() int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("go-kernel:migrate"))
	return int64(h.Sum64()) //nolint:gosec // deliberate wraparound, any stable value works
}()

// Migrate applies all *.sql files from fsys in lexicographic order, once
// each, recording applied versions in schema_migrations. Each file runs in
// its own transaction; files may contain multiple statements. Up-only by
// design.
//
//	//go:embed migrations/*.sql
//	var migrationsFS embed.FS
//
//	sub, _ := fs.Sub(migrationsFS, "migrations")
//	err := db.Migrate(ctx, sub)
func (c *Client) Migrate(ctx context.Context, fsys fs.FS) error {
	files, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return fmt.Errorf("postgres: listing migrations: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("postgres: no *.sql migrations found — wrong fs.Sub path?")
	}
	sort.Strings(files)

	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("postgres: acquiring connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrateLockID); err != nil {
		return fmt.Errorf("postgres: acquiring migration lock: %w", err)
	}
	defer conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrateLockID) //nolint:errcheck

	if _, err := conn.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version    text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("postgres: creating schema_migrations: %w", err)
	}

	applied := map[string]bool{}
	rows, err := conn.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("postgres: reading applied migrations: %w", err)
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return fmt.Errorf("postgres: reading applied migrations: %w", err)
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("postgres: reading applied migrations: %w", err)
	}

	for _, name := range files {
		if applied[name] {
			continue
		}
		sql, err := fs.ReadFile(fsys, name)
		if err != nil {
			return fmt.Errorf("postgres: reading migration %s: %w", name, err)
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("postgres: migration %s: begin: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("postgres: migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("postgres: migration %s: recording version: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("postgres: migration %s: commit: %w", name, err)
		}
	}
	return nil
}
