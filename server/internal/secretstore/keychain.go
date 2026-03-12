package secretstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/zalando/go-keyring"
)

const keychainServicePrefix = "cogitator/"
const keychainIndexAccount = "_index"

// keychainStore is a SecretStore backed by the OS keychain via go-keyring.
//
// Keys within a namespace are stored as individual keychain entries:
//   service = "cogitator/{namespace}"
//   account = key
//   password = value
//
// A separate index entry (account="_index") holds a JSON array of known keys
// so that List can enumerate them without scanning the entire keychain.
// The mutex protects index read-modify-write cycles only; individual key
// reads and writes are safe to issue concurrently without it.
type keychainStore struct {
	mu sync.Mutex // protects index read-modify-write
}

// NewKeychainStore returns a SecretStore backed by the OS keychain.
func NewKeychainStore() SecretStore {
	return &keychainStore{}
}

// serviceName returns the keychain service name for a given namespace.
func serviceName(namespace string) string {
	return keychainServicePrefix + namespace
}

// Get retrieves the value for key in namespace.
// Returns ErrNotFound when the key does not exist.
func (ks *keychainStore) Get(namespace, key string) (string, error) {
	val, err := keyring.Get(serviceName(namespace), key)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("secretstore: keychain get %q/%q: %w", namespace, key, err)
	}
	return val, nil
}

// Set stores or overwrites value for key in namespace, then updates the index.
func (ks *keychainStore) Set(namespace, key, value string) error {
	if err := keyring.Set(serviceName(namespace), key, value); err != nil {
		return fmt.Errorf("secretstore: keychain set %q/%q: %w", namespace, key, err)
	}
	return ks.addToIndex(namespace, key)
}

// Delete removes key from namespace (no-op if absent), then updates the index.
func (ks *keychainStore) Delete(namespace, key string) error {
	err := keyring.Delete(serviceName(namespace), key)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("secretstore: keychain delete %q/%q: %w", namespace, key, err)
	}
	return ks.removeFromIndex(namespace, key)
}

// List returns the keys present in namespace by reading the index and
// reconciling against the keychain (pruning any stale entries).
// Returns an empty (non-nil) slice when no keys exist.
func (ks *keychainStore) List(namespace string) ([]string, error) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	indexed, err := ks.readIndex(namespace)
	if err != nil {
		return nil, err
	}

	// Reconcile: keep only keys that still exist in the keychain.
	live := indexed[:0]
	stale := false
	for _, k := range indexed {
		_, gerr := keyring.Get(serviceName(namespace), k)
		if errors.Is(gerr, keyring.ErrNotFound) {
			stale = true
			continue
		}
		if gerr != nil {
			return nil, fmt.Errorf("secretstore: keychain list reconcile %q/%q: %w", namespace, k, gerr)
		}
		live = append(live, k)
	}

	if stale {
		if werr := ks.writeIndex(namespace, live); werr != nil {
			return nil, werr
		}
	}

	if live == nil {
		live = []string{}
	}
	return live, nil
}

// readIndex reads the JSON key list from the keychain index entry.
// Returns an empty slice when no index exists yet.
// Caller must hold ks.mu.
func (ks *keychainStore) readIndex(namespace string) ([]string, error) {
	raw, err := keyring.Get(serviceName(namespace), keychainIndexAccount)
	if errors.Is(err, keyring.ErrNotFound) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("secretstore: keychain read index %q: %w", namespace, err)
	}
	var keys []string
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		return nil, fmt.Errorf("secretstore: keychain unmarshal index %q: %w", namespace, err)
	}
	return keys, nil
}

// writeIndex persists keys as the JSON index entry.
// Caller must hold ks.mu.
func (ks *keychainStore) writeIndex(namespace string, keys []string) error {
	b, err := json.Marshal(keys)
	if err != nil {
		return fmt.Errorf("secretstore: keychain marshal index %q: %w", namespace, err)
	}
	if err := keyring.Set(serviceName(namespace), keychainIndexAccount, string(b)); err != nil {
		return fmt.Errorf("secretstore: keychain write index %q: %w", namespace, err)
	}
	return nil
}

// addToIndex appends key to the index if not already present.
func (ks *keychainStore) addToIndex(namespace, key string) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	keys, err := ks.readIndex(namespace)
	if err != nil {
		return err
	}
	if slices.Contains(keys, key) {
		return nil // already tracked
	}
	keys = append(keys, key)
	return ks.writeIndex(namespace, keys)
}

// removeFromIndex removes key from the index if present.
func (ks *keychainStore) removeFromIndex(namespace, key string) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	keys, err := ks.readIndex(namespace)
	if err != nil {
		return err
	}
	filtered := keys[:0]
	for _, k := range keys {
		if k != key {
			filtered = append(filtered, k)
		}
	}
	if len(filtered) == len(keys) {
		return nil // key was not in index, nothing to update
	}
	return ks.writeIndex(namespace, filtered)
}
