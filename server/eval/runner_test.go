package eval

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/provider"
)

func TestRunEnrichment(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "eval")
	os.MkdirAll(filepath.Join(dataDir, "enrichment"), 0o755)

	cases := []EnrichmentCase{{
		ID:    "test_001",
		Input: EnrichmentInput{Title: "User likes tea", Content: "Green tea is my favorite."},
		Expected: EnrichmentExpected{
			NodeType:      "preference",
			Tags:          []string{"tea"},
			TagMinOverlap: 0.5,
		},
	}}
	data, _ := json.Marshal(cases)
	os.WriteFile(filepath.Join(dataDir, "enrichment", "cases.json"), data, 0o644)

	mock := provider.NewMock(provider.Response{
		Content: `{"node_type":"preference","summary":"the user likes green tea","tags":["tea","beverage"],"retrieval_triggers":[],"related_nodes":[],"contradictions":[]}`,
	})

	report, err := Run(context.Background(), RunConfig{
		Provider:     mock,
		ProviderName: "mock",
		Model:        "test",
		DataDir:      dataDir,
		Stages:       []string{"enrichment"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Stages) != 1 {
		t.Fatalf("expected 1 stage, got %d", len(report.Stages))
	}
	if report.Stages[0].Metrics["type_accuracy"] != 1.0 {
		t.Errorf("type_accuracy = %f, want 1.0", report.Stages[0].Metrics["type_accuracy"])
	}
}

func TestRunReflection(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "eval")
	os.MkdirAll(filepath.Join(dataDir, "reflection"), 0o755)

	cases := []ReflectionCase{{
		ID: "test_001",
		Messages: []provider.Message{
			{Role: "assistant", Content: "Done."},
			{Role: "user", Content: "That's wrong, I didn't ask for that."},
		},
		ExpectedSignal: "correction",
		MinConfidence:  0.7,
	}}
	data, _ := json.Marshal(cases)
	os.WriteFile(filepath.Join(dataDir, "reflection", "cases.json"), data, 0o644)

	report, err := Run(context.Background(), RunConfig{
		Provider:     provider.NewMock(),
		ProviderName: "mock",
		Model:        "test",
		DataDir:      dataDir,
		Stages:       []string{"reflection"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Stages[0].Metrics["signal_accuracy"] != 1.0 {
		t.Errorf("signal_accuracy = %f, want 1.0", report.Stages[0].Metrics["signal_accuracy"])
	}
}

func TestRunRetrievalVectorPathDeterministic(t *testing.T) {
	dir := t.TempDir()
	rdir := filepath.Join(dir, "retrieval")
	if err := os.MkdirAll(rdir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixtures := `[
	  {"id":"n_coffee","type":"preference","title":"prefers dark roast coffee","summary":"dark roast coffee preference","tags":["coffee","beverage"],"content":"I always pick dark roast coffee."},
	  {"id":"n_hike","type":"fact","title":"enjoys mountain hiking","summary":"mountain hiking hobby","tags":["hiking","outdoors"],"content":"Weekend mountain hiking trips."}
	]`
	cases := `[
	  {"id":"c_coffee","query":"what coffee does the user like dark roast","expected_node_ids":["n_coffee"],"expected_not_ids":["n_hike"],"min_precision":0.5,"min_recall":1.0}
	]`
	if err := os.WriteFile(filepath.Join(rdir, "fixtures.json"), []byte(fixtures), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rdir, "cases.json"), []byte(cases), 0o644); err != nil {
		t.Fatal(err)
	}

	stage, err := runRetrieval(context.Background(), RunConfig{
		Embedder:       NewDeterministicEmbedder(128),
		EmbeddingModel: "det",
	}, filepath.Join(rdir, "cases.json"))
	if err != nil {
		t.Fatalf("runRetrieval: %v", err)
	}
	if len(stage.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(stage.Results))
	}
	r := stage.Results[0]
	if r.Error != "" {
		t.Fatalf("case error: %s", r.Error)
	}
	if r.Scores["recall"] < 1.0 {
		t.Errorf("recall = %v, want 1.0 (coffee node should be retrieved on the vector path)", r.Scores["recall"])
	}
	if r.Scores["exclusion"] != 1.0 {
		t.Errorf("exclusion = %v, want 1.0 (hiking node must not appear)", r.Scores["exclusion"])
	}
}
