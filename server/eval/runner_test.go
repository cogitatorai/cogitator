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
