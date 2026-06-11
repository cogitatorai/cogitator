package api

import (
	"sync"
	"time"
)

// ttlStore is a bounded, single-use nonce/result store with TTL-based eviction.
//
// It exists to replace the previous unbounded package-level maps in the social
// OAuth flow, which (1) grew without limit under an unauthenticated start
// endpoint (memory DoS) and (2) spawned one cleanup goroutine per entry that
// outlived server shutdown (goroutine leak, fired after Stop).
//
// Properties:
//   - entries carry an absolute expiry; expired entries are pruned ON ACCESS
//     (every get/set), so there is NO background goroutine and nothing to wire
//     into the shutdown path;
//   - a hard entry cap bounds memory; when full, the oldest entry (by insertion
//     time) is evicted before a new one is added;
//   - take() is single-use: it removes and returns the entry, mirroring the
//     consume-on-callback semantics these nonce stores require.
//
// It is safe for concurrent use. The clock is injectable for tests.
type ttlStore[V any] struct {
	mu      sync.Mutex
	entries map[string]ttlEntry[V]
	ttl     time.Duration
	max     int
	now     func() time.Time // injectable clock for tests
}

// ttlEntry holds a value alongside its absolute expiry.
type ttlEntry[V any] struct {
	value   V
	expires time.Time
}

// newTTLStore creates a store whose entries live for ttl and whose size is
// capped at max entries.
func newTTLStore[V any](ttl time.Duration, max int) *ttlStore[V] {
	return &ttlStore[V]{
		entries: make(map[string]ttlEntry[V]),
		ttl:     ttl,
		max:     max,
		now:     time.Now,
	}
}

// set stores value under key with a fresh TTL, pruning expired entries and
// evicting the oldest entry if the store is at capacity.
func (s *ttlStore[V]) set(key string, value V) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pruneLocked(now)

	// If this is a new key and we are at capacity, evict the oldest entry so the
	// insert below stays within the cap.
	if _, exists := s.entries[key]; !exists {
		s.evictOldestLocked()
	}
	s.entries[key] = ttlEntry[V]{value: value, expires: now.Add(s.ttl)}
}

// take removes and returns the value for key. ok is false if the key is absent
// or expired (an expired entry is treated as absent and removed). It is
// single-use: a successful take consumes the entry.
func (s *ttlStore[V]) take(key string) (V, bool) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pruneLocked(now)

	e, ok := s.entries[key]
	if !ok {
		var zero V
		return zero, false
	}
	delete(s.entries, key)
	if !now.Before(e.expires) {
		var zero V
		return zero, false
	}
	return e.value, true
}

// pruneLocked removes all expired entries. Caller must hold mu.
func (s *ttlStore[V]) pruneLocked(now time.Time) {
	for k, e := range s.entries {
		if !now.Before(e.expires) {
			delete(s.entries, k)
		}
	}
}

// evictOldestLocked removes the entry with the earliest expiry when the store
// is at or above capacity. Because every entry shares the same TTL, the entry
// with the earliest expiry is also the oldest by insertion time. Caller must
// hold mu.
func (s *ttlStore[V]) evictOldestLocked() {
	if len(s.entries) < s.max {
		return
	}
	var oldestKey string
	var oldest time.Time
	first := true
	for k, e := range s.entries {
		if first || e.expires.Before(oldest) {
			oldestKey = k
			oldest = e.expires
			first = false
		}
	}
	if !first {
		delete(s.entries, oldestKey)
	}
}
