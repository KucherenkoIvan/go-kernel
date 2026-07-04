// Package postgres provides the PostgreSQL client for services built on the
// kernel: a pgx v5 connection pool that implements ddd.TxManager, and the
// Transaction-token resolution used by repositories and readers.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Client wraps a pgx connection pool. It is the production implementation of
// ddd.TxManager — bind it in the composition root:
//
//	db := postgres.MustConnect(ctx, cfg.DatabaseURL)
//	var txManager ddd.TxManager = db
type Client struct {
	pool *pgxpool.Pool
}

// Connect parses the URL, creates a pool, and verifies the connection.
// Pool settings are passed as URL query parameters, e.g.
// postgres://user:pass@host:5432/db?pool_max_conns=10
func Connect(ctx context.Context, url string) (*Client, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("postgres: parsing config: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: creating pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}

	return &Client{pool: pool}, nil
}

// MustConnect is Connect that panics on error — for use in main().
func MustConnect(ctx context.Context, url string) *Client {
	client, err := Connect(ctx, url)
	if err != nil {
		panic(err)
	}
	return client
}

// Pool exposes the underlying pool for infra needs (migrations, health
// checks, LISTEN/NOTIFY). Repositories should use Resolve instead.
func (c *Client) Pool() *pgxpool.Pool { return c.pool }

// Ping verifies the connection — for health checks.
func (c *Client) Ping(ctx context.Context) error { return c.pool.Ping(ctx) }

// Close closes the pool, blocking until all acquired connections are
// released.
func (c *Client) Close() { c.pool.Close() }
