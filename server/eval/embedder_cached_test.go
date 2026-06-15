package eval

import (
	"context"
	"errors"
	"testing"
)

type stubEmbedder struct {
	vec   []float32
	calls int
	err   error
}

func (s *stubEmbedder) Embed(_ context.Context, texts []string, _ string) ([][]float32, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.calls += len(texts)
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = s.vec
	}
	return out, nil
}

func TestCachedEmbedderWritesOnMissThenHits(t *testing.T) {
	dir := t.TempDir()
	stub := &stubEmbedder{vec: []float32{0.1, 0.2, 0.3}}
	e := NewCachedEmbedder(stub, "test-model", dir, false)

	v1, err := e.Embed(context.Background(), []string{"hello world"}, "test-model")
	if err != nil {
		t.Fatalf("miss embed: %v", err)
	}
	if stub.calls != 1 {
		t.Fatalf("expected 1 underlying call, got %d", stub.calls)
	}
	e2 := NewCachedEmbedder(stub, "test-model", dir, false)
	v2, err := e2.Embed(context.Background(), []string{"hello world"}, "test-model")
	if err != nil {
		t.Fatalf("hit embed: %v", err)
	}
	if stub.calls != 1 {
		t.Errorf("cache miss on second call: underlying calls=%d", stub.calls)
	}
	if v1[0][0] != v2[0][0] || len(v2[0]) != 3 {
		t.Errorf("cached vector mismatch: %v vs %v", v1[0], v2[0])
	}
}

func TestCachedEmbedderOfflineMissErrors(t *testing.T) {
	dir := t.TempDir()
	stub := &stubEmbedder{vec: []float32{1}}
	e := NewCachedEmbedder(stub, "test-model", dir, true)

	_, err := e.Embed(context.Background(), []string{"uncached text"}, "test-model")
	if err == nil {
		t.Fatal("expected offline cache-miss error, got nil")
	}
	if stub.calls != 0 {
		t.Errorf("offline mode must not call the underlying embedder, calls=%d", stub.calls)
	}
}

func TestCachedEmbedderPropagatesUnderlyingError(t *testing.T) {
	dir := t.TempDir()
	stub := &stubEmbedder{err: errors.New("api down")}
	e := NewCachedEmbedder(stub, "m", dir, false)
	if _, err := e.Embed(context.Background(), []string{"x"}, "m"); err == nil {
		t.Fatal("expected underlying error to propagate")
	}
}
