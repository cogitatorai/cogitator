package worker

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/database"
	"github.com/cogitatorai/cogitator/server/internal/memory"
	"github.com/cogitatorai/cogitator/server/internal/provider"
)

func TestBackfillEmbedsUnembeddedNodes(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := memory.NewStore(db)

	// Create nodes without embeddings.
	id1, _ := store.CreateNode(&memory.Node{Type: memory.NodeFact, Title: "fact one"})
	id2, _ := store.CreateNode(&memory.Node{Type: memory.NodeFact, Title: "fact two"})

	mock := provider.NewMock(provider.Response{})
	ne := memory.NewNodeEmbedder(store, nil, mock, "test-model", nil)

	count, err := RunBackfill(context.Background(), store, ne, 50)
	if err != nil {
		t.Fatalf("RunBackfill: %v", err)
	}
	if count != 2 {
		t.Errorf("backfilled %d, want 2", count)
	}

	// Verify embeddings exist.
	for _, id := range []string{id1, id2} {
		vec, _ := store.GetEmbedding(id)
		if vec == nil {
			t.Errorf("node %s missing embedding after backfill", id)
		}
	}
}

func TestBackfillSkipsAlreadyEmbedded(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := memory.NewStore(db)

	id1, _ := store.CreateNode(&memory.Node{Type: memory.NodeFact, Title: "already embedded"})
	store.CreateNode(&memory.Node{Type: memory.NodeFact, Title: "not embedded"})

	// Pre-embed the first node.
	store.SaveEmbedding(id1, []float32{1, 0, 0}, "m")

	mock := provider.NewMock(provider.Response{})
	ne := memory.NewNodeEmbedder(store, nil, mock, "test-model", nil)

	count, err := RunBackfill(context.Background(), store, ne, 50)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("backfilled %d, want 1 (should skip already-embedded)", count)
	}
}

func TestBackfillNilEmbedder(t *testing.T) {
	count, err := RunBackfill(context.Background(), nil, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

func TestRunReenrichment(t *testing.T) {
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := memory.NewStore(db)
	eventBus := bus.New()

	for _, nt := range []memory.NodeType{memory.NodePreference, memory.NodeFact, memory.NodePattern, memory.NodeEpisode} {
		store.CreateNode(&memory.Node{
			Type:             nt,
			Title:            string(nt) + " node",
			EnrichmentStatus: memory.EnrichmentComplete,
		})
	}

	count, err := RunReenrichment(context.Background(), store, eventBus, 10)
	if err != nil {
		t.Fatalf("RunReenrichment: %v", err)
	}
	if count != 4 {
		t.Errorf("expected 4 nodes reset, got %d", count)
	}
}

func TestRunContentLengthBackfill(t *testing.T) {
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := memory.NewStore(db)
	cm := memory.NewContentManager(filepath.Join(dir, "memories"))

	id, _ := store.CreateNode(&memory.Node{Type: memory.NodeFact, Title: "test"})
	path, _ := cm.Write(id, "hello world content")
	node, _ := store.GetNode(id)
	node.ContentPath = path
	store.UpdateNode(node)

	count, err := RunContentLengthBackfill(context.Background(), store, cm)
	if err != nil {
		t.Fatalf("RunContentLengthBackfill: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 backfilled, got %d", count)
	}

	var cl sql.NullInt64
	db.QueryRow("SELECT content_length FROM nodes WHERE id = ?", id).Scan(&cl)
	if !cl.Valid || cl.Int64 != int64(len("hello world content")) {
		t.Errorf("content_length = %v, want %d", cl, len("hello world content"))
	}
}
