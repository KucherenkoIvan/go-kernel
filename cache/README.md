# cache

The caching port with two conformance-tested stores. Caching is an adapter concern — the canonical use is a cached Reader decorating the persistent one; application and domain code never see a cache.

- `Store` — get/set (TTL), has, delete-with-count, mget/mset, TTL inspect/update; values are opaque `[]byte`
- `Redis` — production store on go-redis v9 (`NewRedis`, `Ping` for health, `Close`)
- `InMemory` — the test fake *and* the real store for single-binary deployments
- `GetJSON[T]` / `SetJSON[T]` — typed access on top of any store

```go
store, _ := cache.NewRedis(ctx, cfg.RedisURL) // or cache.NewInMemory()

_ = cache.SetJSON(ctx, store, "coupon:overview:c-1", model, 5*time.Minute)
model, ok, _ := cache.GetJSON[CouponOverviewReadModel](ctx, store, "coupon:overview:c-1")
```

Both stores pass the same conformance suite; Redis runs when `TEST_REDIS_URL` is set (`docker run --rm -d -p 6379:6379 redis:7-alpine`).

Guide: [docs/guides/cache.md](../docs/guides/cache.md)
