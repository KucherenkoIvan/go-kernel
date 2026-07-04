package cache

import (
	"context"
	"sync"
	"time"
)

const janitorInterval = 30 * time.Second

// InMemory is a process-local Store: the test fake and the real store for
// single-binary deployments. Expired entries are dropped lazily on access
// and swept by a background janitor (stop it with Close).
type InMemory struct {
	mu       sync.RWMutex
	items    map[string]memEntry
	stop     chan struct{}
	stopOnce sync.Once
}

type memEntry struct {
	value     []byte
	expiresAt time.Time // zero = no expiry
}

func (e memEntry) expired(now time.Time) bool {
	return !e.expiresAt.IsZero() && !now.Before(e.expiresAt)
}

var _ Store = (*InMemory)(nil)

func NewInMemory() *InMemory {
	s := &InMemory{
		items: map[string]memEntry{},
		stop:  make(chan struct{}),
	}
	go s.janitor()
	return s
}

// Close stops the janitor goroutine.
func (s *InMemory) Close() error {
	s.stopOnce.Do(func() { close(s.stop) })
	return nil
}

func (s *InMemory) janitor() {
	ticker := time.NewTicker(janitorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case now := <-ticker.C:
			s.mu.Lock()
			for key, entry := range s.items {
				if entry.expired(now) {
					delete(s.items, key)
				}
			}
			s.mu.Unlock()
		}
	}
}

// get returns a live entry, lazily dropping it if expired.
func (s *InMemory) get(key string) (memEntry, bool) {
	s.mu.RLock()
	entry, ok := s.items[key]
	s.mu.RUnlock()
	if !ok {
		return memEntry{}, false
	}
	if entry.expired(time.Now()) {
		s.mu.Lock()
		delete(s.items, key)
		s.mu.Unlock()
		return memEntry{}, false
	}
	return entry, true
}

func (s *InMemory) Get(_ context.Context, key string) ([]byte, bool, error) {
	entry, ok := s.get(key)
	if !ok {
		return nil, false, nil
	}
	return entry.value, true, nil
}

func (s *InMemory) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	entry := memEntry{value: append([]byte(nil), value...)}
	if ttl > 0 {
		entry.expiresAt = time.Now().Add(ttl)
	}
	s.mu.Lock()
	s.items[key] = entry
	s.mu.Unlock()
	return nil
}

func (s *InMemory) Has(_ context.Context, key string) (bool, error) {
	_, ok := s.get(key)
	return ok, nil
}

func (s *InMemory) Delete(_ context.Context, keys ...string) (int, error) {
	now := time.Now()
	deleted := 0
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range keys {
		if entry, ok := s.items[key]; ok {
			if !entry.expired(now) {
				deleted++
			}
			delete(s.items, key)
		}
	}
	return deleted, nil
}

func (s *InMemory) MGet(_ context.Context, keys ...string) (map[string][]byte, error) {
	out := make(map[string][]byte, len(keys))
	for _, key := range keys {
		if entry, ok := s.get(key); ok {
			out[key] = entry.value
		}
	}
	return out, nil
}

func (s *InMemory) MSet(ctx context.Context, values map[string][]byte, ttl time.Duration) error {
	for key, value := range values {
		if err := s.Set(ctx, key, value, ttl); err != nil {
			return err
		}
	}
	return nil
}

func (s *InMemory) TTL(_ context.Context, key string) (time.Duration, bool, error) {
	entry, ok := s.get(key)
	if !ok {
		return 0, false, nil
	}
	if entry.expiresAt.IsZero() {
		return 0, true, nil
	}
	return time.Until(entry.expiresAt), true, nil
}

func (s *InMemory) UpdateTTL(_ context.Context, key string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.items[key]
	if !ok || entry.expired(time.Now()) {
		delete(s.items, key)
		return false, nil
	}
	if ttl == 0 {
		entry.expiresAt = time.Time{}
	} else {
		entry.expiresAt = time.Now().Add(ttl)
	}
	s.items[key] = entry
	return true, nil
}
