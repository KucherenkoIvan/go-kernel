# sqlite

The embedded database for single-binary deployments — the [`postgres`](../postgres/) client's twin on `modernc.org/sqlite` (pure Go, no cgo, no container; the database is a file inside the process).

- `Open` / `MustOpen` — file or `:memory:`, with service defaults baked in: WAL, `busy_timeout` 5s, enforced foreign keys, `BEGIN IMMEDIATE` write transactions (no upgrade race — same-machine multi-process access is safe)
- `WithinTx` / `Resolve(tx)` — same `ddd.TxManager` token pattern as postgres, `AfterCommit` hooks included
- `Migrate` — the same migration-runner contract, minus the advisory lock a single binary doesn't need
- `Ping` / `Close` — health check and lifecycle

```go
db := sqlite.MustOpen("data/app.db") // ":memory:" in tests — real DB, zero setup
var txManager ddd.TxManager = db
```

The outbox intentionally does **not** exist here — one process is `events.ChannelPublisher` territory.

Guide: [docs/guides/sqlite.md](../docs/guides/sqlite.md)
