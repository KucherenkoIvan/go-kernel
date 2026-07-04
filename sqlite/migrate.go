package sqlite

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
)

// Migrate applies all *.sql files from fsys in lexicographic order, once
// each, recording applied versions in schema_migrations — the postgres
// runner's twin. No advisory lock: sqlite has a single writer and a
// single-binary deployment has a single migrator. Up-only by design.
//
//	//go:embed migrations/*.sql
//	var migrationsFS embed.FS
//
//	sub, _ := fs.Sub(migrationsFS, "migrations")
//	err := db.Migrate(ctx, sub)
func (c *Client) Migrate(ctx context.Context, fsys fs.FS) error {
	files, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return fmt.Errorf("sqlite: listing migrations: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("sqlite: no *.sql migrations found — wrong fs.Sub path?")
	}
	sort.Strings(files)

	if _, err := c.db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version    text PRIMARY KEY,
			applied_at text NOT NULL DEFAULT (datetime('now'))
		)`); err != nil {
		return fmt.Errorf("sqlite: creating schema_migrations: %w", err)
	}

	applied := map[string]bool{}
	rows, err := c.db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("sqlite: reading applied migrations: %w", err)
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return fmt.Errorf("sqlite: reading applied migrations: %w", err)
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("sqlite: reading applied migrations: %w", err)
	}

	for _, name := range files {
		if applied[name] {
			continue
		}
		script, err := fs.ReadFile(fsys, name)
		if err != nil {
			return fmt.Errorf("sqlite: reading migration %s: %w", name, err)
		}

		tx, err := c.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("sqlite: migration %s: begin: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, string(script)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("sqlite: migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("sqlite: migration %s: recording version: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("sqlite: migration %s: commit: %w", name, err)
		}
	}
	return nil
}
