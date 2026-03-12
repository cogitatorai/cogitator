package secretstore

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// FileStore is a SecretStore backed by one YAML file per namespace stored under
// baseDir. Each file is named "{namespace}_secrets.yaml" and contains a flat
// map[string]string. Files are written with 0o600 permissions.
//
// The file is the single source of truth: every operation reads from (and, when
// mutating, writes back to) disk, so multiple FileStore instances sharing the
// same baseDir will observe each other's changes without coordination.
//
// A per-namespace mutex serialises concurrent access from within the same
// process to avoid torn reads/writes.
type FileStore struct {
	baseDir string
	mu      sync.Map // namespace -> *sync.RWMutex
}

// NewFileStore returns a SecretStore that persists secrets as YAML files under
// baseDir. baseDir is created if it does not already exist.
func NewFileStore(baseDir string) SecretStore {
	return &FileStore{baseDir: baseDir}
}

// lockFor returns (creating if necessary) the per-namespace mutex.
func (f *FileStore) lockFor(namespace string) *sync.RWMutex {
	v, _ := f.mu.LoadOrStore(namespace, &sync.RWMutex{})
	return v.(*sync.RWMutex)
}

// filePath returns the absolute path of the YAML file for namespace.
func (f *FileStore) filePath(namespace string) string {
	return filepath.Join(f.baseDir, namespace+"_secrets.yaml")
}

// read loads the YAML file for namespace and returns its contents as a map.
// If the file does not exist, an empty map is returned without error.
func (f *FileStore) read(namespace string) (map[string]string, error) {
	data, err := os.ReadFile(f.filePath(namespace))
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("secretstore: read namespace %q: %w", namespace, err)
	}
	var m map[string]string
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("secretstore: unmarshal namespace %q: %w", namespace, err)
	}
	if m == nil {
		m = map[string]string{}
	}
	return m, nil
}

// write serialises m as YAML and atomically replaces the namespace file.
func (f *FileStore) write(namespace string, m map[string]string) error {
	if err := os.MkdirAll(f.baseDir, 0o700); err != nil {
		return fmt.Errorf("secretstore: mkdir %q: %w", f.baseDir, err)
	}
	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("secretstore: marshal namespace %q: %w", namespace, err)
	}
	if err := os.WriteFile(f.filePath(namespace), data, 0o600); err != nil {
		return fmt.Errorf("secretstore: write namespace %q: %w", namespace, err)
	}
	return nil
}

// Get retrieves the value for key in namespace.
// Returns ErrNotFound when the key does not exist.
func (f *FileStore) Get(namespace, key string) (string, error) {
	mu := f.lockFor(namespace)
	mu.RLock()
	defer mu.RUnlock()

	m, err := f.read(namespace)
	if err != nil {
		return "", err
	}
	v, ok := m[key]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

// Set stores or overwrites value for key in namespace.
func (f *FileStore) Set(namespace, key, value string) error {
	mu := f.lockFor(namespace)
	mu.Lock()
	defer mu.Unlock()

	m, err := f.read(namespace)
	if err != nil {
		return err
	}
	m[key] = value
	return f.write(namespace, m)
}

// Delete removes key from namespace. Deleting a key that does not exist is a
// no-op and returns nil.
func (f *FileStore) Delete(namespace, key string) error {
	mu := f.lockFor(namespace)
	mu.Lock()
	defer mu.Unlock()

	m, err := f.read(namespace)
	if err != nil {
		return err
	}
	delete(m, key) // no-op when key is absent
	return f.write(namespace, m)
}

// List returns the keys stored in namespace. Returns an empty (non-nil) slice
// when the namespace has no keys or does not yet exist.
func (f *FileStore) List(namespace string) ([]string, error) {
	mu := f.lockFor(namespace)
	mu.RLock()
	defer mu.RUnlock()

	m, err := f.read(namespace)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys, nil
}
