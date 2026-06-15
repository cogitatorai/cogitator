package eval

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
)

// DeterministicEmbedder is a hermetic, reproducible provider.Embedder for the
// L1 retrieval-mechanics eval. It needs no API and no network: it hashes
// lowercase whitespace tokens into a fixed-dimension bag-of-words vector and
// L2-normalizes. Identical text always yields the identical vector, and texts
// sharing tokens have higher cosine similarity — enough to author mechanics
// cases with known rankings. It does NOT model real semantic similarity.
type DeterministicEmbedder struct {
	dim int
}

// NewDeterministicEmbedder returns an embedder producing dim-dimensional unit
// vectors. dim<=0 defaults to 128.
func NewDeterministicEmbedder(dim int) *DeterministicEmbedder {
	if dim <= 0 {
		dim = 128
	}
	return &DeterministicEmbedder{dim: dim}
}

// Embed implements provider.Embedder. The model argument is ignored.
func (e *DeterministicEmbedder) Embed(_ context.Context, texts []string, _ string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = e.vector(t)
	}
	return out, nil
}

func (e *DeterministicEmbedder) vector(text string) []float32 {
	v := make([]float32, e.dim)
	for _, tok := range strings.Fields(strings.ToLower(text)) {
		h := fnv.New32a()
		h.Write([]byte(tok))
		v[h.Sum32()%uint32(e.dim)] += 1
	}
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		return v
	}
	inv := float32(1.0 / math.Sqrt(norm))
	for i := range v {
		v[i] *= inv
	}
	return v
}
