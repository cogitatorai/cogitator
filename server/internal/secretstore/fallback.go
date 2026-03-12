package secretstore

import (
	"log/slog"
	"sync"
)

type fallbackStore struct {
	primary  SecretStore
	fallback SecretStore
	once     sync.Once
	active   SecretStore
}

// NewFallbackStore returns a store that uses primary if available, otherwise fallback.
// Availability is determined once at first use via a canary write/read/delete probe.
func NewFallbackStore(primary, fallback SecretStore) SecretStore {
	return &fallbackStore{primary: primary, fallback: fallback}
}

// resolve probes the primary store exactly once and sets active to whichever
// store should be used for all subsequent operations.
func (f *fallbackStore) resolve() SecretStore {
	f.once.Do(func() {
		err := f.primary.Set("_probe", "_health", "ok")
		if err == nil {
			var val string
			val, err = f.primary.Get("_probe", "_health")
			if err == nil && val == "ok" {
				_ = f.primary.Delete("_probe", "_health")
				f.active = f.primary
				slog.Info("secretstore: using OS keychain")
				return
			}
		}
		f.active = f.fallback
		slog.Warn("secretstore: OS keychain unavailable, using file-based fallback")
	})
	return f.active
}

func (f *fallbackStore) Get(namespace, key string) (string, error) {
	return f.resolve().Get(namespace, key)
}

func (f *fallbackStore) Set(namespace, key, value string) error {
	return f.resolve().Set(namespace, key, value)
}

func (f *fallbackStore) Delete(namespace, key string) error {
	return f.resolve().Delete(namespace, key)
}

func (f *fallbackStore) List(namespace string) ([]string, error) {
	return f.resolve().List(namespace)
}
