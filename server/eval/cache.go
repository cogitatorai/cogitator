package eval

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

type Cache struct {
	dir string
}

func NewCache(dir string) *Cache {
	os.MkdirAll(dir, 0o755)
	return &Cache{dir: dir}
}

func CacheKey(prompt, providerName, model string) string {
	h := sha256.New()
	h.Write([]byte(providerName))
	h.Write([]byte{0})
	h.Write([]byte(model))
	h.Write([]byte{0})
	h.Write([]byte(prompt))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (c *Cache) Get(key string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(c.dir, key))
	if err != nil {
		return "", false
	}
	return string(data), true
}

func (c *Cache) Put(key, response string) error {
	return os.WriteFile(filepath.Join(c.dir, key), []byte(response), 0o644)
}

func (c *Cache) Clear() error {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		os.Remove(filepath.Join(c.dir, e.Name()))
	}
	return nil
}
