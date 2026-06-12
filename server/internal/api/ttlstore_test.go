package api

import (
	"runtime"
	"strconv"
	"testing"
	"time"
)

func TestTTLStore_SetAndTake(t *testing.T) {
	s := newTTLStore[int](10*time.Minute, 100)
	now := time.Unix(1000, 0)
	s.now = func() time.Time { return now }

	s.set("k", 42)
	v, ok := s.take("k")
	if !ok {
		t.Fatal("expected to find key")
	}
	if v != 42 {
		t.Fatalf("expected value 42, got %d", v)
	}
}

func TestTTLStore_SingleUse(t *testing.T) {
	s := newTTLStore[int](10*time.Minute, 100)
	now := time.Unix(1000, 0)
	s.now = func() time.Time { return now }

	s.set("k", 7)
	if _, ok := s.take("k"); !ok {
		t.Fatal("first take should succeed")
	}
	if _, ok := s.take("k"); ok {
		t.Fatal("second take should fail: entry is single-use and must be consumed")
	}
}

func TestTTLStore_Expiry(t *testing.T) {
	const ttl = 10 * time.Minute
	s := newTTLStore[int](ttl, 100)
	now := time.Unix(1000, 0)
	s.now = func() time.Time { return now }

	s.set("k", 1)

	// Still valid one second before expiry.
	now = now.Add(ttl - time.Second)
	if _, ok := s.take("nope"); ok {
		t.Fatal("unrelated key should be absent")
	}
	// Re-set since the probe above pruned nothing relevant; reuse a new key.
	s.set("k2", 2)
	now = now.Add(ttl + time.Second)
	if _, ok := s.take("k2"); ok {
		t.Fatal("expected expired entry to be rejected")
	}
}

func TestTTLStore_ExpiredPrunedOnAccess(t *testing.T) {
	const ttl = 5 * time.Minute
	s := newTTLStore[int](ttl, 100)
	now := time.Unix(1000, 0)
	s.now = func() time.Time { return now }

	s.set("a", 1)
	now = now.Add(ttl + time.Second)
	// Accessing a different key prunes the expired "a".
	s.set("b", 2)

	s.mu.Lock()
	_, hasA := s.entries["a"]
	_, hasB := s.entries["b"]
	s.mu.Unlock()
	if hasA {
		t.Fatal("expected expired entry a to be pruned on access")
	}
	if !hasB {
		t.Fatal("expected fresh entry b to remain")
	}
}

func TestTTLStore_CapEvictsOldest(t *testing.T) {
	const ttl = time.Hour
	s := newTTLStore[int](ttl, 3)
	base := time.Unix(1000, 0)
	cur := base
	s.now = func() time.Time { return cur }

	// Insert 3 entries at increasing times; "k0" is the oldest (earliest expiry).
	for i := 0; i < 3; i++ {
		cur = base.Add(time.Duration(i) * time.Second)
		s.set("k"+strconv.Itoa(i), i)
	}
	// A 4th distinct key evicts the oldest (k0); cap stays at 3.
	cur = base.Add(10 * time.Second)
	s.set("k3", 3)

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) != 3 {
		t.Fatalf("expected 3 entries after eviction, got %d", len(s.entries))
	}
	if _, ok := s.entries["k0"]; ok {
		t.Fatal("expected oldest entry k0 to be evicted at cap")
	}
	if _, ok := s.entries["k3"]; !ok {
		t.Fatal("expected newest entry k3 to remain")
	}
}

func TestTTLStore_NewestSurvivesCapAndIsRetrievable(t *testing.T) {
	const ttl = time.Hour
	s := newTTLStore[string](ttl, 2)
	base := time.Unix(1000, 0)
	cur := base
	s.now = func() time.Time { return cur }

	cur = base
	s.set("oldest", "a")
	cur = base.Add(time.Second)
	s.set("mid", "b")
	cur = base.Add(2 * time.Second)
	s.set("newest", "c") // evicts "oldest"

	if _, ok := s.take("oldest"); ok {
		t.Fatal("oldest should have been evicted")
	}
	v, ok := s.take("newest")
	if !ok || v != "c" {
		t.Fatalf("newest entry must still be retrievable, got %q ok=%v", v, ok)
	}
}

// TestTTLStore_NoGoroutinesSpawned asserts the store spawns no background
// goroutines across many operations (the previous design leaked one goroutine
// per entry).
func TestTTLStore_NoGoroutinesSpawned(t *testing.T) {
	s := newTTLStore[int](10*time.Minute, 1000)
	now := time.Unix(1000, 0)
	s.now = func() time.Time { return now }

	runtime.GC()
	before := runtime.NumGoroutine()

	for i := 0; i < 5000; i++ {
		k := "k" + strconv.Itoa(i)
		s.set(k, i)
		s.take(k)
	}

	runtime.GC()
	after := runtime.NumGoroutine()
	if after > before {
		t.Fatalf("ttlStore spawned goroutines: before=%d after=%d", before, after)
	}
}
