package memory

import (
	"math"
	"testing"
)

func TestEmbeddingRoundTrip(t *testing.T) {
	store := NewStore(testDB(t))

	nodeID, err := store.CreateNode(&Node{Type: NodeFact, Title: "test"})
	if err != nil {
		t.Fatal(err)
	}

	vec := []float32{0.1, 0.2, 0.3, 0.4}
	if err := store.SaveEmbedding(nodeID, vec, "text-embedding-3-small"); err != nil {
		t.Fatalf("SaveEmbedding: %v", err)
	}

	got, err := store.GetEmbedding(nodeID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 dims, got %d", len(got))
	}
	for i, v := range vec {
		if math.Abs(float64(got[i]-v)) > 1e-6 {
			t.Errorf("dim %d: got %f, want %f", i, got[i], v)
		}
	}
}

func TestGetAllEmbeddings(t *testing.T) {
	store := NewStore(testDB(t))

	id1, _ := store.CreateNode(&Node{Type: NodeFact, Title: "a"})
	id2, _ := store.CreateNode(&Node{Type: NodeFact, Title: "b"})

	store.SaveEmbedding(id1, []float32{1, 0, 0}, "m")
	store.SaveEmbedding(id2, []float32{0, 1, 0}, "m")

	all, err := store.GetAllEmbeddings("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}
}

func TestGetEmbeddingMissing(t *testing.T) {
	store := NewStore(testDB(t))

	got, err := store.GetEmbedding("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil for missing embedding, got %v", got)
	}
}

func TestDeleteEmbedding(t *testing.T) {
	store := NewStore(testDB(t))

	nodeID, _ := store.CreateNode(&Node{Type: NodeFact, Title: "to delete"})
	store.SaveEmbedding(nodeID, []float32{1, 2, 3}, "m")

	if err := store.DeleteEmbedding(nodeID); err != nil {
		t.Fatalf("DeleteEmbedding: %v", err)
	}

	got, err := store.GetEmbedding(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil after delete, got %v", got)
	}
}

func TestPinnedNodes(t *testing.T) {
	store := NewStore(testDB(t))

	id, _ := store.CreateNode(&Node{Type: NodeFact, Title: "pinned fact", Pinned: true})
	store.CreateNode(&Node{Type: NodeFact, Title: "unpinned fact"})

	pinned, err := store.GetPinnedNodes("")
	if err != nil {
		t.Fatal(err)
	}
	if len(pinned) != 1 {
		t.Fatalf("expected 1 pinned, got %d", len(pinned))
	}
	if pinned[0].ID != id {
		t.Errorf("pinned node ID = %s, want %s", pinned[0].ID, id)
	}
}

func TestGetNodesWithoutEmbeddings(t *testing.T) {
	store := NewStore(testDB(t))

	id1, _ := store.CreateNode(&Node{Type: NodeFact, Title: "a"})
	store.CreateNode(&Node{Type: NodeFact, Title: "b"})

	// Embed only the first one.
	store.SaveEmbedding(id1, []float32{1, 0, 0}, "m")

	nodes, err := store.GetNodesWithoutEmbeddings(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 unembedded node, got %d", len(nodes))
	}
}

func TestGetUnconsolidatedCount(t *testing.T) {
	store := NewStore(testDB(t))

	// Two complete nodes, one still pending.
	n1, _ := store.CreateNode(&Node{Type: NodeFact, Title: "complete a"})
	n2, _ := store.CreateNode(&Node{Type: NodeFact, Title: "complete b"})
	store.CreateNode(&Node{Type: NodeFact, Title: "pending"})

	n, _ := store.GetNode(n1)
	n.EnrichmentStatus = EnrichmentComplete
	store.UpdateNode(n)

	n, _ = store.GetNode(n2)
	n.EnrichmentStatus = EnrichmentComplete
	n.ConsolidatedInto = "some-other-id"
	store.UpdateNode(n)

	count, err := store.GetUnconsolidatedCount()
	if err != nil {
		t.Fatal(err)
	}
	// Only n1 is complete and not consolidated.
	if count != 1 {
		t.Errorf("expected 1 unconsolidated, got %d", count)
	}
}
