package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/provider"
)

func TestNodeEmbedderEmbedsNode(t *testing.T) {
	store := NewStore(testDB(t))
	mock := provider.NewMock(provider.Response{})

	ne := NewNodeEmbedder(store, nil, mock, "test-model", nil)

	nodeID, _ := store.CreateNode(&Node{Type: NodeFact, Title: "coffee preference"})
	node, _ := store.GetNode(nodeID)

	if err := ne.EmbedNode(context.Background(), node); err != nil {
		t.Fatalf("EmbedNode: %v", err)
	}

	vec, err := store.GetEmbedding(nodeID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if vec == nil {
		t.Fatal("expected embedding to be stored")
	}
}

func TestNodeEmbedderNilEmbedder(t *testing.T) {
	ne := NewNodeEmbedder(nil, nil, nil, "", nil)
	err := ne.EmbedNode(context.Background(), &Node{ID: "test", Title: "test"})
	if err != nil {
		t.Errorf("expected nil error for nil embedder, got: %v", err)
	}
}

func TestBuildEmbeddingText(t *testing.T) {
	node := &Node{
		Title:             "User likes coffee",
		Summary:           "Prefers dark roast",
		Tags:              []string{"coffee", "preference"},
		RetrievalTriggers: []string{"coffee", "drink"},
	}
	text := buildEmbeddingText(node)
	if text != "User likes coffee Prefers dark roast coffee preference coffee drink" {
		t.Errorf("unexpected text: %s", text)
	}
}

func TestBuildEmbeddingTextWithContent(t *testing.T) {
	node := &Node{
		Title:   "Test title",
		Summary: "Test summary",
	}
	text := buildEmbeddingTextWithContent(node, "This is the full content of the memory.")
	if !strings.Contains(text, "This is the full content") {
		t.Error("embedding text should include content")
	}
	if !strings.Contains(text, "Test title") {
		t.Error("embedding text should include title")
	}
}

func TestBuildEmbeddingTextContentTruncation(t *testing.T) {
	node := &Node{Title: "title"}
	longContent := strings.Repeat("x", 10000)
	text := buildEmbeddingTextWithContent(node, longContent)
	if len(text) > maxEmbeddingTextLen {
		t.Errorf("text length %d exceeds max %d", len(text), maxEmbeddingTextLen)
	}
}

func TestBuildEmbeddingTextContentEmpty(t *testing.T) {
	node := &Node{
		Title:   "title",
		Summary: "summary",
	}
	text := buildEmbeddingTextWithContent(node, "")
	if text != "title summary" {
		t.Errorf("unexpected text: %q", text)
	}
}
