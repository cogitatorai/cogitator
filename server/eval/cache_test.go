package eval

import (
	"testing"
)

func TestCachePutGet(t *testing.T) {
	c := NewCache(t.TempDir())
	key := CacheKey("test-prompt", "openai", "gpt-4o")
	c.Put(key, "cached response")
	got, ok := c.Get(key)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got != "cached response" {
		t.Errorf("got %q, want %q", got, "cached response")
	}
}

func TestCacheMiss(t *testing.T) {
	c := NewCache(t.TempDir())
	_, ok := c.Get("nonexistent")
	if ok {
		t.Error("expected cache miss")
	}
}

func TestCacheKeyDeterministic(t *testing.T) {
	k1 := CacheKey("prompt", "openai", "gpt-4o")
	k2 := CacheKey("prompt", "openai", "gpt-4o")
	if k1 != k2 {
		t.Error("same inputs should produce same key")
	}
}

func TestCacheKeyVaries(t *testing.T) {
	k1 := CacheKey("prompt", "openai", "gpt-4o")
	k2 := CacheKey("prompt", "openai", "gpt-4.1")
	if k1 == k2 {
		t.Error("different models should produce different keys")
	}
}
