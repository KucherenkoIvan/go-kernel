# Caching

The caching port and its two stores. The kernel side lives in [`cache`](../../cache/README.md).

| Store | Backed by | Use for |
|---|---|---|
| `cache.Redis` | go-redis v9 | multi-replica deployments — replicas share one cache |
| `cache.InMemory` | process memory | tests, and single-binary deployments where a Redis container would be overkill |

Both implement `cache.Store` and pass the same conformance suite, so the choice is a composition-root line — the same swap-the-adapter move as everywhere else in the kernel.

## Where caching lives in the architecture

**Caching is an adapter concern.** The canonical use is a cached Reader decorating the persistent one inside a feature's `adapters/` — application and domain code never see a cache:

```go
// features/coupon/adapters/pg/cached_overview_reader.go
type CachedOverviewReader struct {
	inner ports.CouponOverviewReader // the pg reader
	store cache.Store
	ttl   time.Duration
}

func (r *CachedOverviewReader) GetByID(ctx context.Context, tx ddd.Transaction, id domain.CouponID) (*domain.CouponOverviewReadModel, error) {
	key := "coupon:overview:" + string(id)

	if cached, ok, err := cache.GetJSON[domain.CouponOverviewReadModel](ctx, r.store, key); err == nil && ok {
		return &cached, nil
	} // cache errors fall through to the source of truth

	model, err := r.inner.GetByID(ctx, tx, id)
	if err != nil || model == nil {
		return model, err
	}
	_ = cache.SetJSON(ctx, r.store, key, *model, r.ttl)
	return model, nil
}
```

Swapping the plain reader for the cached one is one line in `feature.go`:

```go
var overviewReader ports.CouponOverviewReader = pg.NewOverviewReader(db)
overviewReader = pg.NewCachedOverviewReader(overviewReader, store, 5*time.Minute)
```

Cache **read-models, never aggregates** — aggregates are loaded for mutation, and a stale aggregate means lost writes; a stale read-model means a slightly old page.

## The Store port

```go
value, ok, err := store.Get(ctx, key)                  // ok=false: miss
err  = store.Set(ctx, key, value, 5*time.Minute)       // ttl 0 = no expiry
n, _ := store.Delete(ctx, "k1", "k2")                  // reports how many existed
m, _ := store.MGet(ctx, "k1", "k2")                    // misses absent from the map
ttl, ok, _ := store.TTL(ctx, key)                      // ok=false: no key; ttl 0 + ok: no expiry
store.UpdateTTL(ctx, key, time.Hour)                   // ttl 0 = remove the expiry
```

Values are opaque `[]byte`; typed access goes through the generic helpers:

```go
err := cache.SetJSON(ctx, store, key, model, ttl)
model, ok, err := cache.GetJSON[CouponOverviewReadModel](ctx, store, key)
```

## Keys, TTLs, invalidation

- **Key convention:** `<feature>:<entity>:<id>` (the service name comes from the Redis DB/namespace, not the key). One place per feature builds keys — a `func overviewKey(id CouponID) string`, never string concatenation at call sites.
- **Every cache entry gets a TTL.** The TTL is the consistency backstop; pick it by "how stale is acceptable", not by load.
- **Invalidate on write where cheap:** the projector or command adapter that changes a read-model deletes its keys (`store.Delete`); the TTL covers everything you forgot.
- Treat the cache as **disposable**: a flush must never break correctness, only warm-up latency.

## Wiring

```go
store, err := cache.NewRedis(ctx, cfg.RedisURL) // or cache.NewInMemory()

// health: the 200 report lists it like every other dependency
checker := health.NewChecker(
	health.Check{Name: "postgres", Check: db.Ping},
	health.Check{Name: "redis", Check: store.Ping},
)

// lifecycle
app.Adapter{Name: "redis", Close: func(context.Context) error { return store.Close() }}
```

## Testing

`cache.NewInMemory()` *is* the fake — no mocks. Code written against `cache.Store` runs identically in tests and in a single-binary deployment. The conformance suite in [`cache/cache_test.go`](../../cache/cache_test.go) runs against a real Redis too — self-provisioned via testcontainers under `make integration`, or your own via `TEST_REDIS_URL`.

## Anti-patterns

| Anti-pattern | Why it is bad | The right way |
|-------------|-------------|---------------|
| Cache calls in application/domain code | Business logic tied to infrastructure | Cached Reader decorator in adapters |
| Caching aggregates | Stale aggregate → lost writes | Cache read-models only |
| Keys without TTL | Unbounded growth, permanent staleness | TTL as the backstop, delete-on-write as the optimization |
| Cache as source of truth | A flush becomes data loss | Cache is disposable; the DB owns the data |
| Key strings built at call sites | Typos create silent misses | One key-builder function per read-model |
| Failing the request on a cache error | Redis down takes the feature down | Fall through to the source of truth |
