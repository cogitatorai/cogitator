package secretstore_test

import (
	"errors"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/secretstore"
)

// brokenStore is a SecretStore that always fails every operation.
type brokenStore struct{}

func (b *brokenStore) Get(namespace, key string) (string, error) {
	return "", errors.New("broken")
}

func (b *brokenStore) Set(namespace, key, value string) error {
	return errors.New("broken")
}

func (b *brokenStore) Delete(namespace, key string) error {
	return errors.New("broken")
}

func (b *brokenStore) List(namespace string) ([]string, error) {
	return nil, errors.New("broken")
}

func TestFallbackStore_UsesPrimaryWhenAvailable(t *testing.T) {
	primaryDir := t.TempDir()
	fallbackDir := t.TempDir()

	primary := secretstore.NewFileStore(primaryDir)
	fallback := secretstore.NewFileStore(fallbackDir)

	fs := secretstore.NewFallbackStore(primary, fallback)

	if err := fs.Set("myapp", "token", "abc123"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Value must be readable from the primary store directly.
	val, err := primary.Get("myapp", "token")
	if err != nil {
		t.Fatalf("primary.Get failed: %v", err)
	}
	if val != "abc123" {
		t.Errorf("expected primary to hold %q, got %q", "abc123", val)
	}

	// Value must NOT be in the fallback store.
	_, err = fallback.Get("myapp", "token")
	if !errors.Is(err, secretstore.ErrNotFound) {
		t.Errorf("expected fallback to return ErrNotFound, got %v", err)
	}
}

func TestFallbackStore_FallsBackWhenPrimaryBroken(t *testing.T) {
	fallbackDir := t.TempDir()
	fallback := secretstore.NewFileStore(fallbackDir)

	fs := secretstore.NewFallbackStore(&brokenStore{}, fallback)

	if err := fs.Set("myapp", "token", "xyz789"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Value must be readable from the fallback store directly.
	val, err := fallback.Get("myapp", "token")
	if err != nil {
		t.Fatalf("fallback.Get failed: %v", err)
	}
	if val != "xyz789" {
		t.Errorf("expected fallback to hold %q, got %q", "xyz789", val)
	}
}

func TestFallbackStore_ProbeRunsOnce(t *testing.T) {
	primaryDir := t.TempDir()
	primary := secretstore.NewFileStore(primaryDir)
	fallbackDir := t.TempDir()
	fallback := secretstore.NewFileStore(fallbackDir)

	fs := secretstore.NewFallbackStore(primary, fallback)

	// Run multiple operations; all should succeed and use the same resolved store.
	for i, tc := range []struct {
		ns, key, val string
	}{
		{"ns1", "k1", "v1"},
		{"ns1", "k2", "v2"},
		{"ns2", "k1", "v3"},
	} {
		if err := fs.Set(tc.ns, tc.key, tc.val); err != nil {
			t.Fatalf("op %d: Set failed: %v", i, err)
		}
		got, err := fs.Get(tc.ns, tc.key)
		if err != nil {
			t.Fatalf("op %d: Get failed: %v", i, err)
		}
		if got != tc.val {
			t.Errorf("op %d: expected %q, got %q", i, tc.val, got)
		}
	}

	keys, err := fs.List("ns1")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 keys in ns1, got %d", len(keys))
	}

	if err := fs.Delete("ns1", "k1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	_, err = fs.Get("ns1", "k1")
	if !errors.Is(err, secretstore.ErrNotFound) {
		t.Errorf("expected ErrNotFound after Delete, got %v", err)
	}
}
