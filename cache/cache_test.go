package cache_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/KucherenkoIvan/go-kernel/cache"
	"github.com/KucherenkoIvan/go-kernel/ddd"
)

// The same conformance suite runs against both stores — InMemory always,
// Redis when TEST_REDIS_URL is set, e.g.:
//
//	docker run --rm -d -p 6379:6379 redis:7-alpine
//	TEST_REDIS_URL=redis://localhost:6379/0 go test ./cache/
func TestInMemory_Conformance(t *testing.T) {
	store := cache.NewInMemory()
	t.Cleanup(func() { _ = store.Close() })
	conformance(t, store)
}

func TestRedis_Conformance(t *testing.T) {
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL is not set")
	}
	store, err := cache.NewRedis(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	conformance(t, store)
}

// key returns a unique key so runs never collide on a shared Redis.
func key(s string) string { return "gokernel-test:" + ddd.NewEntityID() + ":" + s }

func conformance(t *testing.T, store cache.Store) {
	t.Helper()
	ctx := context.Background()

	t.Run("get/set roundtrip", func(t *testing.T) {
		k := key("roundtrip")
		if _, ok, err := store.Get(ctx, k); err != nil || ok {
			t.Fatalf("missing key: ok=%v err=%v", ok, err)
		}
		if err := store.Set(ctx, k, []byte("v1"), 0); err != nil {
			t.Fatal(err)
		}
		value, ok, err := store.Get(ctx, k)
		if err != nil || !ok || string(value) != "v1" {
			t.Fatalf("got %q ok=%v err=%v", value, ok, err)
		}
	})

	t.Run("expiry", func(t *testing.T) {
		k := key("expiry")
		if err := store.Set(ctx, k, []byte("v"), 80*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if _, ok, _ := store.Get(ctx, k); !ok {
			t.Fatal("must exist before expiry")
		}
		time.Sleep(150 * time.Millisecond)
		if _, ok, _ := store.Get(ctx, k); ok {
			t.Fatal("must be gone after expiry")
		}
	})

	t.Run("has and delete count", func(t *testing.T) {
		k1, k2, k3 := key("d1"), key("d2"), key("d3")
		_ = store.Set(ctx, k1, []byte("1"), 0)
		_ = store.Set(ctx, k2, []byte("2"), 0)

		if ok, _ := store.Has(ctx, k1); !ok {
			t.Fatal("Has(k1) = false")
		}
		n, err := store.Delete(ctx, k1, k2, k3)
		if err != nil || n != 2 {
			t.Fatalf("deleted %d err=%v, want 2", n, err)
		}
		if ok, _ := store.Has(ctx, k1); ok {
			t.Fatal("k1 survived delete")
		}
	})

	t.Run("mget/mset", func(t *testing.T) {
		k1, k2, missing := key("m1"), key("m2"), key("m3")
		err := store.MSet(ctx, map[string][]byte{k1: []byte("a"), k2: []byte("b")}, 0)
		if err != nil {
			t.Fatal(err)
		}
		got, err := store.MGet(ctx, k1, k2, missing)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || string(got[k1]) != "a" || string(got[k2]) != "b" {
			t.Fatalf("got %v", got)
		}
		if _, present := got[missing]; present {
			t.Fatal("missing key must be absent from the map")
		}
	})

	t.Run("ttl inspection and update", func(t *testing.T) {
		k := key("ttl")

		if _, ok, _ := store.TTL(ctx, k); ok {
			t.Fatal("TTL of missing key must report ok=false")
		}

		_ = store.Set(ctx, k, []byte("v"), 0)
		ttl, ok, err := store.TTL(ctx, k)
		if err != nil || !ok || ttl != 0 {
			t.Fatalf("no-expiry key: ttl=%v ok=%v err=%v, want 0,true", ttl, ok, err)
		}

		if ok, _ := store.UpdateTTL(ctx, k, time.Minute); !ok {
			t.Fatal("UpdateTTL on existing key must report true")
		}
		ttl, ok, _ = store.TTL(ctx, k)
		if !ok || ttl <= 30*time.Second || ttl > time.Minute {
			t.Fatalf("ttl after update = %v", ttl)
		}

		if ok, _ := store.UpdateTTL(ctx, k, 0); !ok {
			t.Fatal("UpdateTTL(0) must persist the key")
		}
		if ttl, ok, _ = store.TTL(ctx, k); !ok || ttl != 0 {
			t.Fatalf("persisted key: ttl=%v ok=%v, want 0,true", ttl, ok)
		}

		if ok, _ := store.UpdateTTL(ctx, key("nope"), time.Minute); ok {
			t.Fatal("UpdateTTL on missing key must report false")
		}
	})
}

type overview struct {
	ID    string
	Count int
}

func TestJSONHelpers(t *testing.T) {
	store := cache.NewInMemory()
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	k := key("json")

	if _, ok, err := cache.GetJSON[overview](ctx, store, k); ok || err != nil {
		t.Fatalf("missing: ok=%v err=%v", ok, err)
	}
	if err := cache.SetJSON(ctx, store, k, overview{ID: "o-1", Count: 3}, time.Minute); err != nil {
		t.Fatal(err)
	}
	got, ok, err := cache.GetJSON[overview](ctx, store, k)
	if err != nil || !ok || got.ID != "o-1" || got.Count != 3 {
		t.Fatalf("got %+v ok=%v err=%v", got, ok, err)
	}
}
