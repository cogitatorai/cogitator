package eval

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cogitatorai/cogitator/server/internal/provider"
)

// CachedEmbedder wraps a real provider.Embedder with a committed on-disk vector
// cache so L2 retrieval evals are reproducible and runnable offline. Cache files
// live under <dir>/<model>/<sha256(text)>.json (one JSON float array each) and
// are committed to git. On a cache miss in offline mode it errors instead of
// calling the network, keeping the committed cache complete.
type CachedEmbedder struct {
	inner   provider.Embedder
	model   string
	dir     string
	offline bool
}

// NewCachedEmbedder wraps inner, caching vectors under dir/<model>/. When
// offline is true, a cache miss is an error (no network call).
func NewCachedEmbedder(inner provider.Embedder, model, dir string, offline bool) *CachedEmbedder {
	return &CachedEmbedder{inner: inner, model: model, dir: dir, offline: offline}
}

func (e *CachedEmbedder) pathFor(text string) string {
	sum := sha256.Sum256([]byte(text))
	return filepath.Join(e.dir, e.model, fmt.Sprintf("%x.json", sum))
}

// Embed implements provider.Embedder, one text at a time so each gets its own
// cache entry. The model arg is ignored in favor of the configured model.
func (e *CachedEmbedder) Embed(ctx context.Context, texts []string, _ string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		if v, ok := e.read(t); ok {
			out[i] = v
			continue
		}
		if e.offline {
			sum := sha256.Sum256([]byte(t))
			return nil, fmt.Errorf("offline: no cached embedding for text (sha %x...); run locally with an API key to regenerate and commit the cache", sum[:6])
		}
		vecs, err := e.inner.Embed(ctx, []string{t}, e.model)
		if err != nil {
			return nil, err
		}
		if len(vecs) == 0 {
			return nil, fmt.Errorf("embedder returned no vector for text")
		}
		if err := e.write(t, vecs[0]); err != nil {
			return nil, fmt.Errorf("write embedding cache: %w", err)
		}
		out[i] = vecs[0]
	}
	return out, nil
}

func (e *CachedEmbedder) read(text string) ([]float32, bool) {
	data, err := os.ReadFile(e.pathFor(text))
	if err != nil {
		return nil, false
	}
	var v []float32
	if json.Unmarshal(data, &v) != nil {
		return nil, false
	}
	return v, true
}

func (e *CachedEmbedder) write(text string, v []float32) error {
	p := e.pathFor(text)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}
