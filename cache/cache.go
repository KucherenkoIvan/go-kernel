// Package cache provides the caching port and its two stores: Redis and
// in-memory. The in-memory store doubles as the test fake and as the real
// store for single-binary deployments.
//
// Caching is an adapter concern: the canonical use is a cached Reader
// decorating the persistent one inside a feature's adapters/ — application
// and domain code never see a cache.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Store is the caching port. Values are opaque bytes — typed access goes
// through GetJSON/SetJSON (or your own serialization on top).
//
// TTL semantics: ttl == 0 means "no expiry" everywhere.
type Store interface {
	// Get returns the value and whether the key exists.
	Get(ctx context.Context, key string) (value []byte, ok bool, err error)
	// Set stores the value; ttl == 0 keeps it until deleted.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	// Has reports whether the key exists.
	Has(ctx context.Context, key string) (bool, error)
	// Delete removes keys, reporting how many existed.
	Delete(ctx context.Context, keys ...string) (int, error)

	// MGet returns the values that exist; missing keys are absent from the map.
	MGet(ctx context.Context, keys ...string) (map[string][]byte, error)
	// MSet stores all values with one shared ttl.
	MSet(ctx context.Context, values map[string][]byte, ttl time.Duration) error

	// TTL returns the remaining time-to-live. ok == false: no such key;
	// ok == true with ttl == 0: the key has no expiry.
	TTL(ctx context.Context, key string) (ttl time.Duration, ok bool, err error)
	// UpdateTTL sets a new ttl on an existing key, reporting whether it existed.
	UpdateTTL(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

// GetJSON reads and unmarshals a cached value.
func GetJSON[T any](ctx context.Context, s Store, key string) (T, bool, error) {
	var zero T
	raw, ok, err := s.Get(ctx, key)
	if err != nil || !ok {
		return zero, false, err
	}
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return zero, false, fmt.Errorf("cache: decoding %s: %w", key, err)
	}
	return value, true, nil
}

// SetJSON marshals and stores a value.
func SetJSON[T any](ctx context.Context, s Store, key string, value T, ttl time.Duration) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cache: encoding %s: %w", key, err)
	}
	return s.Set(ctx, key, raw, ttl)
}
