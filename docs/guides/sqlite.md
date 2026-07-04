# SQLite (single-binary deployments)

The embedded-database client for the smallest deployments. The kernel side lives in [`sqlite`](../../sqlite/README.md) — the [postgres client's](./postgres.md) twin on [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite).

## Why (and when)

SQLite is not a server — it is a library. With modernc's pure-Go build the entire engine is **compiled into your binary**: no cgo, no C toolchain, no container, trivial cross-compilation. The deployment story becomes *one static executable and one file next to it*.

| Choose | When |
|---|---|
| `sqlite` | one process, one box: personal tools, small services, prototypes that should survive |
| [`postgres`](./postgres.md) | multiple replicas, multiple services on one DB, outbox, heavy concurrent writes |

The architecture makes this a swap, not a rewrite: domain, application, and ports are untouched — a feature gets a sqlite twin of its pg adapter and the composition root picks one.

## Usage

Identical shape to postgres — token, `Resolve`, `AfterCommit`, migrations:

```go
db := sqlite.MustOpen("data/app.db") // ":memory:" for tests
defer db.Close()

migrations, _ := fs.Sub(migrationsFS, "migrations")
if err := db.Migrate(ctx, migrations); err != nil { ... }

var txManager ddd.TxManager = db
```

```go
// a repository — same pattern, database/sql surface
func (r *CouponRepository) GetByID(ctx context.Context, tx ddd.Transaction, id domain.CouponID) (*domain.Coupon, error) {
	row := r.db.Resolve(tx).QueryRowContext(ctx,
		`SELECT id, code, usage_limit, used_count FROM coupons WHERE id = $1`, string(id))
	// ...
}
```

`Open` applies service defaults: WAL journaling (readers don't block the writer), `busy_timeout` 5s (concurrent writers wait instead of failing with SQLITE_BUSY), enforced foreign keys, and **immediate write transactions** — `WithinTx` takes the write lock up front (`BEGIN IMMEDIATE`), which eliminates the deferred read→write upgrade race. That race is nastier than an error: without it, concurrent read-then-write transactions silently lose updates (test-pinned in `concurrency_test.go`).

## Multi-process access (same machine)

WAL exists precisely so multiple *local* processes can share one file — readers never block, writers queue on `busy_timeout`, and immediate transactions remove the upgrade race. So a long-running process and CLI one-shots on the same database are **safe**, given the defaults `Open` sets and short write transactions. What remains unsafe is sharing across *machines or replicas*: WAL coordinates through shared memory (the `-shm` file), which does not work over NFS or between pods — that's postgres territory.

Wiring is the standard set:

```go
health.Check{Name: "sqlite", Check: db.Ping}
app.Adapter{Name: "sqlite", Close: func(context.Context) error { return db.Close() }}
```

## What carries over from postgres — and what doesn't

**Carries over unchanged:**
- `ddd.TxManager` / token / `Resolve` — use-cases don't know which database they run on.
- `AfterCommit` hooks — `events.ChannelPublisher` delivers commit-gated events exactly as on postgres. A single-process deployment is the channel publisher's home turf.
- The migration runner (`db.Migrate`, same lexicographic `*.sql` + `schema_migrations` contract; no advisory lock — a single binary has a single migrator).

**Doesn't carry over:**
- **The outbox** — it needs advisory locks and `SKIP LOCKED`. By design: if the deployment is small enough for SQLite, cross-service durable events aren't its problem; the channel publisher covers same-process reactions.
- **Repositories verbatim** — `sqlite.Querier` is `database/sql`-shaped (`ExecContext`/`QueryContext`), not pgx-shaped, and dialects differ at the edges. `$1` placeholders work in both, but there is no `jsonb`, `timestamptz` is text/integer, and `ON CONFLICT` support is narrower. Adapter-rewrite territory — cheap by design.

## Testing

The best part: `sqlite.Open(":memory:")` gives every test a real database with zero setup — no containers, no env gating, no skips. For repository tests this is often *better* than fakes: real SQL runs against a real engine in microseconds. (In-memory databases pin the pool to one connection under the hood — each pooled connection would otherwise get its own private database.)

## Anti-patterns

| Anti-pattern | The right way |
|-------------|---------------|
| Sharing a SQLite file across machines/replicas (NFS, pods) | That is postgres's job — WAL coordination is same-machine only |
| Long write transactions with concurrent local processes | Short transactions; compute before, write inside |
| Porting pg repositories by find-and-replace | Write the sqlite adapter twin consciously; dialects differ at the edges |
| Skipping WAL/busy_timeout pragmas with a hand-rolled DSN | Use `sqlite.Open` — the defaults are the fix for SQLITE_BUSY |
| Reaching for the outbox on sqlite | `events.ChannelPublisher` — same port, right guarantee for one process |
