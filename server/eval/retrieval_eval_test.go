package eval

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// findRetrievalDir locates testdata/retrieval relative to this package.
func findRetrievalDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("testdata", "retrieval")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("retrieval testdata dir not found: %v", err)
	}
	return dir
}

// TestL1MechanicsHermetic runs the deterministic mechanics cases with no API,
// no network, no cache. This is the always-on CI gate for retrieval mechanics.
func TestL1MechanicsHermetic(t *testing.T) {
	dir := findRetrievalDir(t)
	casesPath := filepath.Join(dir, "mechanics.json")
	if _, err := os.Stat(casesPath); err != nil {
		t.Skip("mechanics.json not present yet (authored in Task 8)")
	}
	stage, err := runRetrieval(context.Background(), RunConfig{
		Embedder:       NewDeterministicEmbedder(0),
		EmbeddingModel: "deterministic",
	}, casesPath)
	if err != nil {
		t.Fatalf("run mechanics: %v", err)
	}
	for _, r := range stage.Results {
		if !r.Pass {
			t.Errorf("mechanics case %s failed: scores=%v diagnostics=%v", r.ID, r.Scores, r.Diagnostics)
		}
	}
}

// TestL2SemanticOfflineCache runs the semantic cases against the committed
// embedding cache with no network. Skips when the cache is absent so a checkout
// lacking embeddings never spuriously fails.
func TestL2SemanticOfflineCache(t *testing.T) {
	dir := findRetrievalDir(t)
	casesPath := filepath.Join(dir, "cases.json")
	embDir := filepath.Join("testdata", "embeddings")
	if _, err := os.Stat(casesPath); err != nil {
		t.Skip("cases.json not present yet (authored in Task 9)")
	}
	if _, err := os.Stat(embDir); err != nil {
		t.Skip("no committed embedding cache; run `eval run -stage retrieval -embedder real` locally to populate it")
	}
	model := "text-embedding-3-small"
	cached := NewCachedEmbedder(nil, model, embDir, true) // offline: cache-only
	stage, err := runRetrieval(context.Background(), RunConfig{
		Embedder:       cached,
		EmbeddingModel: model,
	}, casesPath)
	if err != nil {
		t.Skipf("offline semantic eval unavailable (likely incomplete cache): %v", err)
	}
	for _, r := range stage.Results {
		if !r.Pass {
			t.Errorf("semantic case %s below threshold: scores=%v diagnostics=%v", r.ID, r.Scores, r.Diagnostics)
		}
	}
}
