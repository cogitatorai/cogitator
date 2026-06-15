package eval

import (
	"context"
	"math"
	"testing"
)

func TestDeterministicEmbedderStableAndNormalized(t *testing.T) {
	e := NewDeterministicEmbedder(64)
	v1, err := e.Embed(context.Background(), []string{"dark roast coffee"}, "det")
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	v2, _ := e.Embed(context.Background(), []string{"dark roast coffee"}, "det")
	if len(v1) != 1 || len(v1[0]) != 64 {
		t.Fatalf("dims: got %d vecs, len %d", len(v1), len(v1[0]))
	}
	for i := range v1[0] {
		if v1[0][i] != v2[0][i] {
			t.Fatalf("non-deterministic at %d: %v vs %v", i, v1[0][i], v2[0][i])
		}
	}
	var sum float64
	for _, x := range v1[0] {
		sum += float64(x) * float64(x)
	}
	if math.Abs(sum-1.0) > 1e-5 {
		t.Errorf("not normalized: |v|^2=%v", sum)
	}
}

func TestDeterministicEmbedderSimilarityOrdering(t *testing.T) {
	e := NewDeterministicEmbedder(128)
	q, _ := e.Embed(context.Background(), []string{"coffee preference dark roast"}, "det")
	near, _ := e.Embed(context.Background(), []string{"dark roast coffee"}, "det")
	far, _ := e.Embed(context.Background(), []string{"hiking trails mountains"}, "det")
	simNear := dot(q[0], near[0])
	simFar := dot(q[0], far[0])
	if simNear <= simFar {
		t.Errorf("expected coffee query closer to coffee node: near=%v far=%v", simNear, simFar)
	}
}

func dot(a, b []float32) float64 {
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

func TestDeterministicEmbedderEmptyText(t *testing.T) {
	e := NewDeterministicEmbedder(16)
	v, err := e.Embed(context.Background(), []string{""}, "det")
	if err != nil {
		t.Fatalf("embed empty: %v", err)
	}
	if len(v) != 1 || len(v[0]) != 16 {
		t.Fatalf("empty text should still yield a zero vector of right dim, got len %d", len(v[0]))
	}
}
