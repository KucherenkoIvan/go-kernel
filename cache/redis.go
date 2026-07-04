package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis is the production Store backed by go-redis.
type Redis struct {
	client *redis.Client
}

var _ Store = (*Redis)(nil)

// NewRedis connects using a URL, e.g. redis://:password@localhost:6379/0,
// and verifies the connection.
func NewRedis(ctx context.Context, url string) (*Redis, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("cache: parsing redis url: %w", err)
	}
	client := redis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("cache: redis ping: %w", err)
	}
	return &Redis{client: client}, nil
}

// Ping verifies connectivity — for health checks.
func (r *Redis) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

// Close releases the connection pool.
func (r *Redis) Close() error {
	return r.client.Close()
}

func (r *Redis) Get(ctx context.Context, key string) ([]byte, bool, error) {
	value, err := r.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("cache: redis get: %w", err)
	}
	return value, true, nil
}

func (r *Redis) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := r.client.Set(ctx, key, value, ttl).Err(); err != nil {
		return fmt.Errorf("cache: redis set: %w", err)
	}
	return nil
}

func (r *Redis) Has(ctx context.Context, key string) (bool, error) {
	n, err := r.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("cache: redis exists: %w", err)
	}
	return n > 0, nil
}

func (r *Redis) Delete(ctx context.Context, keys ...string) (int, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	n, err := r.client.Del(ctx, keys...).Result()
	if err != nil {
		return 0, fmt.Errorf("cache: redis del: %w", err)
	}
	return int(n), nil
}

func (r *Redis) MGet(ctx context.Context, keys ...string) (map[string][]byte, error) {
	if len(keys) == 0 {
		return map[string][]byte{}, nil
	}
	values, err := r.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("cache: redis mget: %w", err)
	}
	out := make(map[string][]byte, len(keys))
	for i, v := range values {
		if s, ok := v.(string); ok {
			out[keys[i]] = []byte(s)
		}
	}
	return out, nil
}

func (r *Redis) MSet(ctx context.Context, values map[string][]byte, ttl time.Duration) error {
	if len(values) == 0 {
		return nil
	}
	// MSET has no TTL — pipeline individual SETs instead
	pipe := r.client.Pipeline()
	for k, v := range values {
		pipe.Set(ctx, k, v, ttl)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("cache: redis mset: %w", err)
	}
	return nil
}

func (r *Redis) TTL(ctx context.Context, key string) (time.Duration, bool, error) {
	ttl, err := r.client.PTTL(ctx, key).Result()
	if err != nil {
		return 0, false, fmt.Errorf("cache: redis pttl: %w", err)
	}
	// go-redis passes PTTL's sentinels through as raw -2/-1 (not scaled to ms)
	switch ttl {
	case -2: // redis: key does not exist
		return 0, false, nil
	case -1: // redis: no expiry
		return 0, true, nil
	default:
		return ttl, true, nil
	}
}

func (r *Redis) UpdateTTL(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if ttl == 0 {
		ok, err := r.client.Persist(ctx, key).Result()
		if err != nil {
			return false, fmt.Errorf("cache: redis persist: %w", err)
		}
		if ok {
			return true, nil
		}
		// PERSIST returns false both for "no key" and "no expiry to remove"
		return r.Has(ctx, key)
	}
	ok, err := r.client.PExpire(ctx, key, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("cache: redis pexpire: %w", err)
	}
	return ok, nil
}
