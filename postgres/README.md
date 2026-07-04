# postgres

The PostgreSQL client for services: a pgx v5 pool that **is** the `ddd.TxManager`.

- `Connect` / `MustConnect` — pool from a URL (pool params as query args), verified with a ping
- `WithinTx` — one transaction per callback, rollback on error, `AfterCommit` hooks honored
- `Resolve(tx)` — token → `Querier` (the tx, or the pool for `ddd.NoTransaction`); shaped to fit scany and sqlc
- `Migrate` — lexicographic `*.sql` from an `embed.FS`, `schema_migrations` bookkeeping, advisory-locked across replicas, up-only
- `Ping` — for `health.Check`

```go
db := postgres.MustConnect(ctx, cfg.DatabaseURL)
defer db.Close()

err := db.WithinTx(ctx, func(tx ddd.Transaction) error {
	if err := repo.Save(ctx, tx, order); err != nil {
		return err
	}
	return producer.Publish(ctx, tx, order.PopEvents())
})
```

Integration tests are gated on `TEST_POSTGRES_URL` (`docker run --rm -d -p 5432:5432 -e POSTGRES_PASSWORD=test postgres:17-alpine`).

Guide: [docs/guides/postgres.md](../docs/guides/postgres.md) · Embedded twin: [`sqlite`](../sqlite/)
