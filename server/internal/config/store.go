package config

import (
	"os"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/cogitatorai/cogitator/server/internal/secretstore"
)

// Store provides thread-safe access to the running configuration
// and persists changes to the config file on disk.
type Store struct {
	mu    sync.RWMutex
	cfg   *Config
	path  string
	store secretstore.SecretStore
}

// NewStore creates a store backed by the given file path.
// If path is empty, changes are held in memory only.
func NewStore(cfg *Config, path string, store secretstore.SecretStore) *Store {
	return &Store{cfg: cfg, path: path, store: store}
}

// Get returns a deep copy of the current config.
func (s *Store) Get() *Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := *s.cfg
	if s.cfg.Providers != nil {
		cp.Providers = make(map[string]ProviderConfig, len(s.cfg.Providers))
		for k, v := range s.cfg.Providers {
			cp.Providers[k] = v
		}
	}
	return &cp
}

// Save replaces the current config and writes it to disk.
// Secrets (API keys, bot tokens) are written to a separate file with
// restricted permissions. The main config file never contains secrets
// because the relevant struct fields carry yaml:"-" tags.
func (s *Store) Save(cfg *Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Always extract and persist secrets to the dedicated store first.
	sec := ExtractSecrets(cfg)
	if s.store != nil {
		if err := SaveSecretsToStore(s.store, sec); err != nil {
			return err
		}
	}

	if s.path != "" {
		// Strip secrets before writing the main config so they never
		// appear in cogitator.yaml. Restore them afterward so the
		// in-memory Config remains complete.
		clearSecrets(cfg)
		data, err := yaml.Marshal(cfg)
		ApplySecrets(cfg, sec)
		if err != nil {
			return err
		}
		if err := os.WriteFile(s.path, data, 0o644); err != nil {
			return err
		}
	}

	s.cfg = cfg
	return nil
}

// SecretStore returns the underlying SecretStore.
func (s *Store) SecretStore() secretstore.SecretStore { return s.store }

// MergeAllowedDomains adds the given domains to Security.AllowedDomains,
// deduplicating against existing entries, persists the result to disk,
// and returns the merged list.
func (s *Store) MergeAllowedDomains(domains []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing := make(map[string]bool, len(s.cfg.Security.AllowedDomains))
	for _, d := range s.cfg.Security.AllowedDomains {
		existing[d] = true
	}

	var added bool
	for _, d := range domains {
		if d != "" && !existing[d] {
			existing[d] = true
			s.cfg.Security.AllowedDomains = append(s.cfg.Security.AllowedDomains, d)
			added = true
		}
	}

	if added && s.path != "" {
		sec := ExtractSecrets(s.cfg)
		clearSecrets(s.cfg)
		data, err := yaml.Marshal(s.cfg)
		ApplySecrets(s.cfg, sec)
		if err != nil {
			return s.cfg.Security.AllowedDomains, err
		}
		if err := os.WriteFile(s.path, data, 0o644); err != nil {
			return s.cfg.Security.AllowedDomains, err
		}
	}

	// Return a copy so callers can't mutate our slice.
	merged := make([]string, len(s.cfg.Security.AllowedDomains))
	copy(merged, s.cfg.Security.AllowedDomains)
	return merged, nil
}
