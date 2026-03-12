package secretstore

import (
	"errors"
	"os"
	"testing"
)

func skipWithoutKeychain(t *testing.T) {
	t.Helper()
	if os.Getenv("TEST_KEYCHAIN") == "" {
		t.Skip("set TEST_KEYCHAIN=1 to run keychain tests")
	}
}

func TestKeychainStore_SetGetDelete(t *testing.T) {
	skipWithoutKeychain(t)

	const ns = "test-keychain-setgetdelete"
	const key = "mykey"
	const val = "mysecret"

	s := NewKeychainStore()

	// Clean up before and after in case a previous run left debris.
	_ = s.Delete(ns, key)
	t.Cleanup(func() { _ = s.Delete(ns, key) })

	if err := s.Set(ns, key, val); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := s.Get(ns, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != val {
		t.Errorf("Get returned %q, want %q", got, val)
	}

	if err := s.Delete(ns, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = s.Get(ns, key)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete returned %v, want ErrNotFound", err)
	}
}

func TestKeychainStore_List(t *testing.T) {
	skipWithoutKeychain(t)

	const ns = "test-keychain-list"
	keys := []string{"alpha", "beta"}

	s := NewKeychainStore()

	// Clean up before and after.
	for _, k := range keys {
		_ = s.Delete(ns, k)
	}
	t.Cleanup(func() {
		for _, k := range keys {
			_ = s.Delete(ns, k)
		}
	})

	for _, k := range keys {
		if err := s.Set(ns, k, "value-"+k); err != nil {
			t.Fatalf("Set %q: %v", k, err)
		}
	}

	listed, err := s.List(ns)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != len(keys) {
		t.Errorf("List returned %d keys, want %d: %v", len(listed), len(keys), listed)
	}

	// Verify each expected key appears in the list.
	listed_set := make(map[string]bool, len(listed))
	for _, k := range listed {
		listed_set[k] = true
	}
	for _, k := range keys {
		if !listed_set[k] {
			t.Errorf("List missing expected key %q", k)
		}
	}
}

func TestKeychainStore_GetMissing(t *testing.T) {
	skipWithoutKeychain(t)

	s := NewKeychainStore()

	_, err := s.Get("test-keychain-missing-ns", "no-such-key")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get on missing key returned %v, want ErrNotFound", err)
	}
}
