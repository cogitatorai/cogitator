package secretstore_test

import (
	"errors"
	"sort"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/secretstore"
)

// newStore creates a FileStore rooted at a temp directory unique to the test.
func newStore(t *testing.T) secretstore.SecretStore {
	t.Helper()
	return secretstore.NewFileStore(t.TempDir())
}

func TestFileStore_SetGetDelete(t *testing.T) {
	s := newStore(t)

	if err := s.Set("myapp", "token", "abc123"); err != nil {
		t.Fatalf("Set: unexpected error: %v", err)
	}

	got, err := s.Get("myapp", "token")
	if err != nil {
		t.Fatalf("Get: unexpected error: %v", err)
	}
	if got != "abc123" {
		t.Errorf("Get: want %q, got %q", "abc123", got)
	}

	if err := s.Delete("myapp", "token"); err != nil {
		t.Fatalf("Delete: unexpected error: %v", err)
	}

	_, err = s.Get("myapp", "token")
	if !errors.Is(err, secretstore.ErrNotFound) {
		t.Errorf("Get after Delete: want ErrNotFound, got %v", err)
	}
}

func TestFileStore_List(t *testing.T) {
	s := newStore(t)

	// Populate two separate namespaces.
	namespaceA := "svc-a"
	namespaceB := "svc-b"

	keysA := []string{"alpha", "beta", "gamma"}
	for _, k := range keysA {
		if err := s.Set(namespaceA, k, "val-"+k); err != nil {
			t.Fatalf("Set(%s, %s): %v", namespaceA, k, err)
		}
	}
	if err := s.Set(namespaceB, "delta", "val-delta"); err != nil {
		t.Fatalf("Set(%s, delta): %v", namespaceB, err)
	}

	got, err := s.List(namespaceA)
	if err != nil {
		t.Fatalf("List(%s): %v", namespaceA, err)
	}
	sort.Strings(got)
	sort.Strings(keysA)
	if len(got) != len(keysA) {
		t.Fatalf("List(%s): want %v, got %v", namespaceA, keysA, got)
	}
	for i := range keysA {
		if got[i] != keysA[i] {
			t.Errorf("List(%s)[%d]: want %q, got %q", namespaceA, i, keysA[i], got[i])
		}
	}

	// Namespace B must only contain its own key, not namespace A's keys.
	gotB, err := s.List(namespaceB)
	if err != nil {
		t.Fatalf("List(%s): %v", namespaceB, err)
	}
	if len(gotB) != 1 || gotB[0] != "delta" {
		t.Errorf("List(%s): want [delta], got %v", namespaceB, gotB)
	}
}

func TestFileStore_Persistence(t *testing.T) {
	dir := t.TempDir()

	// First store instance writes a value.
	s1 := secretstore.NewFileStore(dir)
	if err := s1.Set("persist-ns", "api_key", "super-secret"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Second store instance (same dir) must see the written value.
	s2 := secretstore.NewFileStore(dir)
	got, err := s2.Get("persist-ns", "api_key")
	if err != nil {
		t.Fatalf("Get on second instance: %v", err)
	}
	if got != "super-secret" {
		t.Errorf("Get on second instance: want %q, got %q", "super-secret", got)
	}
}

func TestFileStore_GetMissing(t *testing.T) {
	s := newStore(t)

	_, err := s.Get("ns", "nonexistent")
	if !errors.Is(err, secretstore.ErrNotFound) {
		t.Errorf("Get missing key: want ErrNotFound, got %v", err)
	}
}

func TestFileStore_ListEmpty(t *testing.T) {
	s := newStore(t)

	keys, err := s.List("empty-ns")
	if err != nil {
		t.Fatalf("List empty namespace: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("List empty namespace: want [], got %v", keys)
	}
}

func TestFileStore_DeleteNonexistent(t *testing.T) {
	s := newStore(t)

	// Deleting a key that was never written must not error.
	if err := s.Delete("ns", "ghost"); err != nil {
		t.Errorf("Delete nonexistent key: want nil, got %v", err)
	}

	// Deleting a key twice (second time it is already gone) must not error.
	if err := s.Set("ns", "real", "value"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Delete("ns", "real"); err != nil {
		t.Fatalf("First Delete: %v", err)
	}
	if err := s.Delete("ns", "real"); err != nil {
		t.Errorf("Second Delete of already-deleted key: want nil, got %v", err)
	}
}
