// Package sqlite provides the embedded-database client for single-binary
// deployments: the postgres client's twin on modernc.org/sqlite (pure Go, no
// cgo, no container — the database is a file inside the process, or memory).
//
// It implements the same ddd.TxManager / token / Resolve pattern, including
// AfterCommit hooks, so events.ChannelPublisher and everything written
// against the ports works unchanged. What does NOT carry over from postgres:
// the outbox (needs advisory locks and SKIP LOCKED — and a single-process
// deployment is exactly where the channel publisher already suffices).
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver
)

// Client wraps the embedded database. It is the production implementation
// of ddd.TxManager for sqlite-backed services:
//
//	db := sqlite.MustOpen("data/app.db")
//	var txManager ddd.TxManager = db
type Client struct {
	db *sql.DB
}

// Open opens (creating if missing) the database file and applies the
// service defaults: WAL journaling (readers don't block the writer),
// busy_timeout 5s (writers wait instead of failing with SQLITE_BUSY), and
// enforced foreign keys. Use ":memory:" for an in-memory database (tests).
func Open(path string) (*Client, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: opening %s: %w", path, err)
	}

	if strings.Contains(path, ":memory:") {
		// each pooled connection would get its own private memory database —
		// pin the pool to a single connection
		db.SetMaxOpenConns(1)
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: opening %s: %w", path, err)
	}
	return &Client{db: db}, nil
}

// MustOpen is Open that panics on error — for use in main().
func MustOpen(path string) *Client {
	client, err := Open(path)
	if err != nil {
		panic(err)
	}
	return client
}

// DB exposes the underlying pool for infra needs. Repositories should use
// Resolve instead.
func (c *Client) DB() *sql.DB { return c.db }

// Ping verifies the database is usable — for health checks.
func (c *Client) Ping(ctx context.Context) error { return c.db.PingContext(ctx) }

// Close closes the database.
func (c *Client) Close() error { return c.db.Close() }
