package memory

import (
	"context"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/provider"
)

func TestNodeEmbedderEmbedsNode(t *testing.T) {
	store := NewStore(testDB(t))
	mock := provider.NewMock(provider.Response{})

	ne := NewNodeEmbedder(store, mock, "test-model", nil)

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
	ne := NewNodeEmbedder(nil, nil, "", nil)
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
