package memory

import (
	"path/filepath"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/database"
)

func testDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCreateAndGetNode(t *testing.T) {
	store := NewStore(testDB(t))

	node := &Node{
		Type:       NodeFact,
		Title:      "User uses Go",
		Confidence: 0.8,
	}

	id, err := store.CreateNode(node)
	if err != nil {
		t.Fatalf("CreateNode() error: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID")
	}

	got, err := store.GetNode(id)
	if err != nil {
		t.Fatalf("GetNode() error: %v", err)
	}
	if got.Title != "User uses Go" {
		t.Errorf("expected title 'User uses Go', got %q", got.Title)
	}
	if got.EnrichmentStatus != EnrichmentPending {
		t.Errorf("expected enrichment status 'pending', got %q", got.EnrichmentStatus)
	}
	if got.Confidence != 0.8 {
		t.Errorf("expected confidence 0.8, got %f", got.Confidence)
	}
}

func TestGetNodeNotFound(t *testing.T) {
	store := NewStore(testDB(t))

	_, err := store.GetNode("nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateNode(t *testing.T) {
	store := NewStore(testDB(t))

	id, _ := store.CreateNode(&Node{
		Type:       NodeFact,
		Title:      "Original",
		Confidence: 0.5,
	})

	node, _ := store.GetNode(id)
	node.Title = "Updated"
	node.Summary = "Updated summary"
	node.Tags = []string{"go", "programming"}
	node.EnrichmentStatus = EnrichmentComplete

	err := store.UpdateNode(node)
	if err != nil {
		t.Fatalf("UpdateNode() error: %v", err)
	}

	got, _ := store.GetNode(id)
	if got.Title != "Updated" {
		t.Errorf("expected 'Updated', got %q", got.Title)
	}
	if got.Summary != "Updated summary" {
		t.Errorf("expected 'Updated summary', got %q", got.Summary)
	}
	if len(got.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(got.Tags))
	}
	if got.EnrichmentStatus != EnrichmentComplete {
		t.Errorf("expected 'complete', got %q", got.EnrichmentStatus)
	}
}

func TestCreateEdge(t *testing.T) {
	store := NewStore(testDB(t))

	id1, _ := store.CreateNode(&Node{Type: NodeFact, Title: "A", Confidence: 0.5})
	id2, _ := store.CreateNode(&Node{Type: NodeFact, Title: "B", Confidence: 0.5})

	err := store.CreateEdge(&Edge{
		SourceID: id1,
		TargetID: id2,
		Relation: RelSupports,
		Weight:   0.8,
	})
	if err != nil {
		t.Fatalf("CreateEdge() error: %v", err)
	}

	fromEdges, err := store.GetEdgesFrom(id1, "")
	if err != nil {
		t.Fatalf("GetEdgesFrom() error: %v", err)
	}
	if len(fromEdges) != 1 {
		t.Fatalf("expected 1 edge from, got %d", len(fromEdges))
	}
	if fromEdges[0].Relation != RelSupports {
		t.Errorf("expected relation 'supports', got %q", fromEdges[0].Relation)
	}

	toEdges, err := store.GetEdgesTo(id2, "")
	if err != nil {
		t.Fatalf("GetEdgesTo() error: %v", err)
	}
	if len(toEdges) != 1 {
		t.Fatalf("expected 1 edge to, got %d", len(toEdges))
	}
}

func TestListNodesByType(t *testing.T) {
	store := NewStore(testDB(t))

	store.CreateNode(&Node{Type: NodeFact, Title: "Fact 1", Confidence: 0.5})
	store.CreateNode(&Node{Type: NodeFact, Title: "Fact 2", Confidence: 0.5})
	store.CreateNode(&Node{Type: NodePreference, Title: "Pref 1", Confidence: 0.5})

	facts, err := store.ListNodes("", NodeFact, 100, 0)
	if err != nil {
		t.Fatalf("ListNodes() error: %v", err)
	}
	if len(facts) != 2 {
		t.Errorf("expected 2 facts, got %d", len(facts))
	}

	prefs, _ := store.ListNodes("", NodePreference, 100, 0)
	if len(prefs) != 1 {
		t.Errorf("expected 1 preference, got %d", len(prefs))
	}
}

func TestGetPendingEnrichment(t *testing.T) {
	store := NewStore(testDB(t))

	store.CreateNode(&Node{Type: NodeFact, Title: "Pending", Confidence: 0.5})
	store.CreateNode(&Node{Type: NodeFact, Title: "Also pending", Confidence: 0.5})

	pending, err := store.GetPendingEnrichment(10)
	if err != nil {
		t.Fatalf("GetPendingEnrichment() error: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("expected 2 pending, got %d", len(pending))
	}
}

func TestDeleteNode(t *testing.T) {
	store := NewStore(testDB(t))

	id, _ := store.CreateNode(&Node{Type: NodeFact, Title: "To delete", Confidence: 0.5})
	err := store.DeleteNode(id)
	if err != nil {
		t.Fatalf("DeleteNode() error: %v", err)
	}

	_, err = store.GetNode(id)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteNodeCascadesEdges(t *testing.T) {
	store := NewStore(testDB(t))

	id1, _ := store.CreateNode(&Node{Type: NodeFact, Title: "A", Confidence: 0.5})
	id2, _ := store.CreateNode(&Node{Type: NodeFact, Title: "B", Confidence: 0.5})
	store.CreateEdge(&Edge{SourceID: id1, TargetID: id2, Relation: RelSupports, Weight: 0.5})

	store.DeleteNode(id1)

	edges, _ := store.GetEdgesTo(id2, "")
	if len(edges) != 0 {
		t.Errorf("expected 0 edges after cascade, got %d", len(edges))
	}
}

func TestGetNodeSummaries(t *testing.T) {
	store := NewStore(testDB(t))

	store.CreateNode(&Node{Type: NodeFact, Title: "Fact A", Summary: "Summary A", Confidence: 0.9})
	store.CreateNode(&Node{Type: NodePreference, Title: "Pref B", Summary: "Summary B", Confidence: 0.5})

	summaries, err := store.GetNodeSummaries("")
	if err != nil {
		t.Fatalf("GetNodeSummaries() error: %v", err)
	}
	if len(summaries) != 2 {
		t.Errorf("expected 2 summaries, got %d", len(summaries))
	}
	// Should be ordered by confidence DESC
	if summaries[0].Title != "Fact A" {
		t.Errorf("expected highest confidence first, got %q", summaries[0].Title)
	}
}

func TestGetNodeSummariesByType(t *testing.T) {
	store := NewStore(testDB(t))

	store.CreateNode(&Node{Type: NodeFact, Title: "Fact", Confidence: 0.5})
	store.CreateNode(&Node{Type: NodePreference, Title: "Pref", Confidence: 0.5})
	store.CreateNode(&Node{Type: NodePattern, Title: "Pattern", Confidence: 0.5})

	summaries, err := store.GetNodeSummaries("", NodeFact, NodePreference)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(summaries) != 2 {
		t.Errorf("expected 2, got %d", len(summaries))
	}
}

func TestGetConnectedNodes(t *testing.T) {
	store := NewStore(testDB(t))

	id1, _ := store.CreateNode(&Node{Type: NodeFact, Title: "Center", Confidence: 0.5})
	id2, _ := store.CreateNode(&Node{Type: NodeFact, Title: "Related", Confidence: 0.5})
	id3, _ := store.CreateNode(&Node{Type: NodeFact, Title: "Unrelated", Confidence: 0.5})

	store.CreateEdge(&Edge{SourceID: id1, TargetID: id2, Relation: RelSupports, Weight: 0.8})

	connected, err := store.GetConnectedNodes(id1, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(connected) != 1 {
		t.Fatalf("expected 1 connected, got %d", len(connected))
	}
	if connected[0].Title != "Related" {
		t.Errorf("expected 'Related', got %q", connected[0].Title)
	}
	_ = id3
}

func TestDeleteEdge(t *testing.T) {
	store := NewStore(testDB(t))

	id1, _ := store.CreateNode(&Node{Type: NodeFact, Title: "A", Confidence: 0.5})
	id2, _ := store.CreateNode(&Node{Type: NodeFact, Title: "B", Confidence: 0.5})
	store.CreateEdge(&Edge{SourceID: id1, TargetID: id2, Relation: RelSupports, Weight: 0.5})

	err := store.DeleteEdge(id1, id2, RelSupports)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	edges, _ := store.GetEdgesFrom(id1, "")
	if len(edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(edges))
	}
}

func TestTouchAccess(t *testing.T) {
	store := NewStore(testDB(t))

	id, _ := store.CreateNode(&Node{Type: NodeFact, Title: "Access me", Confidence: 0.5})

	node, _ := store.GetNode(id)
	if node.LastAccessed != nil {
		t.Error("expected nil LastAccessed initially")
	}

	store.TouchAccess(id)

	node, _ = store.GetNode(id)
	if node.LastAccessed == nil {
		t.Error("expected non-nil LastAccessed after touch")
	}
}

func TestStats(t *testing.T) {
	store := NewStore(testDB(t))

	id1, _ := store.CreateNode(&Node{Type: NodeFact, Title: "A", Confidence: 0.5})
	id2, _ := store.CreateNode(&Node{Type: NodeFact, Title: "B", Confidence: 0.5})
	store.CreateEdge(&Edge{SourceID: id1, TargetID: id2, Relation: RelSupports, Weight: 0.5})

	stats, err := store.Stats()
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if stats["total_nodes"] != 2 {
		t.Errorf("expected 2 nodes, got %d", stats["total_nodes"])
	}
	if stats["total_edges"] != 1 {
		t.Errorf("expected 1 edge, got %d", stats["total_edges"])
	}
	if stats["pending_enrichment"] != 2 {
		t.Errorf("expected 2 pending, got %d", stats["pending_enrichment"])
	}
}

// ptr returns a pointer to the given string value.
func ptr(s string) *string { return &s }

// insertTestUser creates a minimal user row so FK constraints are satisfied.
func insertTestUser(t *testing.T, db *database.DB, id string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO users (id, email, name, password_hash, role) VALUES (?, ?, ?, ?, ?)`,
		id, id, id, "hash", "user")
	if err != nil {
		t.Fatalf("insertTestUser(%s): %v", id, err)
	}
}

func TestCreateNode_SharedMode(t *testing.T) {
	store := NewStore(testDB(t))

	id, err := store.CreateNode(&Node{
		Type:       NodeFact,
		Title:      "Shared fact",
		Confidence: 0.8,
	})
	if err != nil {
		t.Fatalf("CreateNode() error: %v", err)
	}

	got, err := store.GetNode(id)
	if err != nil {
		t.Fatalf("GetNode() error: %v", err)
	}
	if got.UserID != nil {
		t.Errorf("expected nil UserID for shared node, got %q", *got.UserID)
	}
}

func TestCreateNode_PrivateMode(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)

	userID := "user-alice"
	insertTestUser(t, db, userID)
	id, err := store.CreateNode(&Node{
		Type:       NodeFact,
		Title:      "Alice's private fact",
		Confidence: 0.8,
		UserID:     &userID,
	})
	if err != nil {
		t.Fatalf("CreateNode() error: %v", err)
	}

	got, err := store.GetNode(id)
	if err != nil {
		t.Fatalf("GetNode() error: %v", err)
	}
	if got.UserID == nil {
		t.Fatal("expected non-nil UserID for private node")
	}
	if *got.UserID != userID {
		t.Errorf("expected UserID %q, got %q", userID, *got.UserID)
	}
}

func TestListNodes_VisibilityRule(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	insertTestUser(t, db, "alice")
	insertTestUser(t, db, "bob")

	// Create: 1 shared, 1 private for Alice, 1 private for Bob.
	store.CreateNode(&Node{Type: NodeFact, Title: "Shared", Confidence: 0.5})
	store.CreateNode(&Node{Type: NodeFact, Title: "Alice private", Confidence: 0.5, UserID: ptr("alice"), Private: true})
	store.CreateNode(&Node{Type: NodeFact, Title: "Bob private", Confidence: 0.5, UserID: ptr("bob"), Private: true})

	// Alice sees shared + own = 2.
	alice, err := store.ListNodes("alice", "", 100, 0)
	if err != nil {
		t.Fatalf("ListNodes(alice) error: %v", err)
	}
	if len(alice) != 2 {
		t.Errorf("alice: expected 2 nodes, got %d", len(alice))
	}
	for _, n := range alice {
		if n.UserID != nil && *n.UserID == "bob" {
			t.Error("alice must not see bob's private node")
		}
	}

	// Bob sees shared + own = 2.
	bob, err := store.ListNodes("bob", "", 100, 0)
	if err != nil {
		t.Fatalf("ListNodes(bob) error: %v", err)
	}
	if len(bob) != 2 {
		t.Errorf("bob: expected 2 nodes, got %d", len(bob))
	}
	for _, n := range bob {
		if n.UserID != nil && *n.UserID == "alice" {
			t.Error("bob must not see alice's private node")
		}
	}

	// Empty userID sees all 3 (worker mode).
	all, err := store.ListNodes("", "", 100, 0)
	if err != nil {
		t.Fatalf("ListNodes('') error: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("worker: expected 3 nodes, got %d", len(all))
	}
}

func TestGetNodeSummaries_VisibilityRule(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	insertTestUser(t, db, "alice")
	insertTestUser(t, db, "bob")

	store.CreateNode(&Node{Type: NodeFact, Title: "Shared", Confidence: 0.9})
	store.CreateNode(&Node{Type: NodeFact, Title: "Alice private", Confidence: 0.5, UserID: ptr("alice"), Private: true})
	store.CreateNode(&Node{Type: NodeFact, Title: "Bob private", Confidence: 0.5, UserID: ptr("bob"), Private: true})

	alice, err := store.GetNodeSummaries("alice")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(alice) != 2 {
		t.Errorf("alice: expected 2 summaries, got %d", len(alice))
	}

	all, err := store.GetNodeSummaries("")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("worker: expected 3 summaries, got %d", len(all))
	}
}

func TestGetAllEmbeddings_VisibilityRule(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	insertTestUser(t, db, "alice")
	insertTestUser(t, db, "bob")

	sharedID, _ := store.CreateNode(&Node{Type: NodeFact, Title: "Shared"})
	aliceID, _ := store.CreateNode(&Node{Type: NodeFact, Title: "Alice", UserID: ptr("alice"), Private: true})
	bobID, _ := store.CreateNode(&Node{Type: NodeFact, Title: "Bob", UserID: ptr("bob"), Private: true})

	store.SaveEmbedding(sharedID, []float32{1, 0, 0}, "m")
	store.SaveEmbedding(aliceID, []float32{0, 1, 0}, "m")
	store.SaveEmbedding(bobID, []float32{0, 0, 1}, "m")

	alice, err := store.GetAllEmbeddings("alice")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(alice) != 2 {
		t.Errorf("alice: expected 2 embeddings, got %d", len(alice))
	}
	if _, ok := alice[bobID]; ok {
		t.Error("alice must not see bob's embedding")
	}

	all, err := store.GetAllEmbeddings("")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("worker: expected 3 embeddings, got %d", len(all))
	}
}

func TestGetPinnedNodes_VisibilityRule(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	insertTestUser(t, db, "alice")
	insertTestUser(t, db, "bob")

	store.CreateNode(&Node{Type: NodeFact, Title: "Shared pinned", Pinned: true})
	store.CreateNode(&Node{Type: NodeFact, Title: "Alice pinned", Pinned: true, UserID: ptr("alice"), Private: true})
	store.CreateNode(&Node{Type: NodeFact, Title: "Bob pinned", Pinned: true, UserID: ptr("bob"), Private: true})

	alice, err := store.GetPinnedNodes("alice")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(alice) != 2 {
		t.Errorf("alice: expected 2 pinned, got %d", len(alice))
	}
	for _, n := range alice {
		if n.UserID != nil && *n.UserID == "bob" {
			t.Error("alice must not see bob's pinned node")
		}
	}

	all, err := store.GetPinnedNodes("")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("worker: expected 3 pinned, got %d", len(all))
	}
}

func TestGetEmbeddingsWithMeta(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)

	// Create nodes of different types.
	idFact, _ := store.CreateNode(&Node{Type: NodeFact, Title: "fact node"})
	idPref, _ := store.CreateNode(&Node{Type: NodePreference, Title: "pref node"})
	idEpisode, _ := store.CreateNode(&Node{Type: NodeEpisode, Title: "episode node"})

	// Store embeddings for all three.
	vec := []float32{0.1, 0.2, 0.3}
	store.SaveEmbedding(idFact, vec, "test-model")
	store.SaveEmbedding(idPref, vec, "test-model")
	store.SaveEmbedding(idEpisode, vec, "test-model")

	// Set content_length on fact node.
	store.UpdateContentLength(idFact, 400)

	// Retrieve only fact and preference types.
	results, err := store.GetEmbeddingsWithMeta("", []NodeType{NodeFact, NodePreference})
	if err != nil {
		t.Fatalf("GetEmbeddingsWithMeta: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Check that fact node has content_length set.
	factMeta, ok := results[idFact]
	if !ok {
		t.Fatal("fact node not in results")
	}
	if factMeta.Type != NodeFact {
		t.Errorf("fact type = %s, want %s", factMeta.Type, NodeFact)
	}
	if factMeta.ContentLength != 400 {
		t.Errorf("fact content_length = %d, want 400", factMeta.ContentLength)
	}

	// Check that preference node has zero content_length (NULL).
	prefMeta, ok := results[idPref]
	if !ok {
		t.Fatal("pref node not in results")
	}
	if prefMeta.ContentLength != 0 {
		t.Errorf("pref content_length = %d, want 0", prefMeta.ContentLength)
	}

	// Episode should be absent.
	if _, ok := results[idEpisode]; ok {
		t.Error("episode should not be in results")
	}
}

func TestGetEdgesFrom_VisibilityRule(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	insertTestUser(t, db, "alice")
	insertTestUser(t, db, "bob")

	// Source node is shared.
	srcID, _ := store.CreateNode(&Node{Type: NodeFact, Title: "Source"})
	// Targets: shared, alice-private, bob-private.
	sharedTarget, _ := store.CreateNode(&Node{Type: NodeFact, Title: "Shared target"})
	aliceTarget, _ := store.CreateNode(&Node{Type: NodeFact, Title: "Alice target", UserID: ptr("alice"), Private: true})
	bobTarget, _ := store.CreateNode(&Node{Type: NodeFact, Title: "Bob target", UserID: ptr("bob"), Private: true})

	store.CreateEdge(&Edge{SourceID: srcID, TargetID: sharedTarget, Relation: RelSupports, Weight: 0.8})
	store.CreateEdge(&Edge{SourceID: srcID, TargetID: aliceTarget, Relation: RelSupports, Weight: 0.8})
	store.CreateEdge(&Edge{SourceID: srcID, TargetID: bobTarget, Relation: RelSupports, Weight: 0.8})

	// Alice sees edges to shared + alice's target = 2.
	alice, err := store.GetEdgesFrom(srcID, "alice")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(alice) != 2 {
		t.Errorf("alice: expected 2 edges, got %d", len(alice))
	}
	for _, e := range alice {
		if e.TargetID == bobTarget {
			t.Error("alice must not see edge to bob's target")
		}
	}

	// Worker sees all 3.
	all, err := store.GetEdgesFrom(srcID, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("worker: expected 3 edges, got %d", len(all))
	}
}

func TestGetEmbeddingsByTypeAndOwner(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)

	insertTestUser(t, db, "user1")
	userID := "user1"
	idPrivate, _ := store.CreateNode(&Node{Type: NodeFact, Title: "private fact", UserID: &userID, Private: true})
	idShared, _ := store.CreateNode(&Node{Type: NodeFact, Title: "shared fact"})
	idPref, _ := store.CreateNode(&Node{Type: NodePreference, Title: "pref"})

	vec := []float32{0.1, 0.2, 0.3}
	store.SaveEmbedding(idPrivate, vec, "test")
	store.SaveEmbedding(idShared, vec, "test")
	store.SaveEmbedding(idPref, vec, "test")

	results, err := store.GetEmbeddingsByTypeAndOwner(NodeFact, &userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 fact embeddings, got %d", len(results))
	}
	if _, ok := results[idPref]; ok {
		t.Error("preference should not be in fact results")
	}

	shared, err := store.GetEmbeddingsByTypeAndOwner(NodeFact, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(shared) != 1 {
		t.Errorf("expected 1 shared fact, got %d", len(shared))
	}
}

func TestListNodesByTags(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)

	id1, _ := store.CreateNode(&Node{Type: NodeFact, Title: "birthday", Tags: []string{"birthday", "identity"}})
	store.CreateNode(&Node{Type: NodeFact, Title: "coffee preference", Tags: []string{"coffee", "food"}})

	nodes, err := store.ListNodesByTags("", NodeFact, []string{"birthday", "identity"})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(nodes))
	}
	if len(nodes) > 0 && nodes[0].ID != id1 {
		t.Errorf("expected node %s, got %s", id1, nodes[0].ID)
	}
}

func TestAdjustConfidence(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)

	id, _ := store.CreateNode(&Node{Type: NodeFact, Title: "test", Confidence: 0.7})

	store.AdjustConfidence(id, 0.02, 0.95)
	node, _ := store.GetNode(id)
	if node.Confidence != 0.72 {
		t.Errorf("confidence after boost = %f, want 0.72", node.Confidence)
	}

	store.AdjustConfidence(id, 1.0, 0.95)
	node, _ = store.GetNode(id)
	if node.Confidence != 0.95 {
		t.Errorf("confidence after cap = %f, want 0.95", node.Confidence)
	}

	store.AdjustConfidence(id, -2.0, 0.1)
	node, _ = store.GetNode(id)
	if node.Confidence != 0.1 {
		t.Errorf("confidence after floor = %f, want 0.1", node.Confidence)
	}
}

func TestNodePrivacyField(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	insertTestUser(t, db, "user1")

	uid := "user1"
	id, err := store.CreateNode(&Node{
		Type: NodeFact, Title: "private fact", UserID: &uid, Private: true, Confidence: 0.9,
	})
	if err != nil {
		t.Fatal(err)
	}

	node, err := store.GetNode(id)
	if err != nil {
		t.Fatal(err)
	}
	if !node.Private {
		t.Error("expected Private=true after create")
	}

	node.Private = false
	store.UpdateNode(node)

	updated, _ := store.GetNode(id)
	if updated.Private {
		t.Error("expected Private=false after update")
	}
}

func TestEdgePrivacyDerivedFromNodes(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	insertTestUser(t, db, "user1")

	uid := "user1"
	idPublic, _ := store.CreateNode(&Node{Type: NodeFact, Title: "public", UserID: &uid, Confidence: 0.9})
	idPrivate, _ := store.CreateNode(&Node{Type: NodeFact, Title: "private", UserID: &uid, Private: true, Confidence: 0.9})
	idPublic2, _ := store.CreateNode(&Node{Type: NodeFact, Title: "public2", UserID: &uid, Confidence: 0.9})

	store.CreateEdge(&Edge{SourceID: idPublic, TargetID: idPublic2, Relation: RelRelatedTo, Weight: 0.5})
	edges, _ := store.GetEdgesFrom(idPublic, uid)
	if len(edges) > 0 && edges[0].Private {
		t.Error("edge between public nodes should be public")
	}

	store.CreateEdge(&Edge{SourceID: idPublic, TargetID: idPrivate, Relation: RelSupports, Weight: 0.5})
	edges2, _ := store.GetEdgesFrom(idPublic, uid)
	for _, e := range edges2 {
		if e.TargetID == idPrivate && !e.Private {
			t.Error("edge touching private node should be private")
		}
	}
}

func TestSetNodeVisibilityCascadesToEdges(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	insertTestUser(t, db, "user1")

	uid := "user1"
	idA, _ := store.CreateNode(&Node{Type: NodeFact, Title: "A", UserID: &uid, Confidence: 0.9})
	idB, _ := store.CreateNode(&Node{Type: NodeFact, Title: "B", UserID: &uid, Confidence: 0.9})
	store.CreateEdge(&Edge{SourceID: idA, TargetID: idB, Relation: RelRelatedTo, Weight: 0.5})

	store.SetNodeVisibility(idA, true)
	node, _ := store.GetNode(idA)
	if !node.Private {
		t.Error("node should be private")
	}
	edges, _ := store.GetEdgesFrom(idA, uid)
	if len(edges) > 0 && !edges[0].Private {
		t.Error("edge should cascade to private")
	}

	store.SetNodeVisibility(idA, false)
	edges, _ = store.GetEdgesFrom(idA, uid)
	if len(edges) > 0 && edges[0].Private {
		t.Error("edge should cascade back to public")
	}
}

func TestPrivateNodeVisibility(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	insertTestUser(t, db, "user1")
	insertTestUser(t, db, "user2")

	uid1 := "user1"
	uid2 := "user2"

	store.CreateNode(&Node{Type: NodeFact, Title: "user1 secret", UserID: &uid1, Private: true, Confidence: 0.9})
	store.CreateNode(&Node{Type: NodeFact, Title: "shared fact", UserID: &uid1, Confidence: 0.9})

	nodes1, _ := store.ListNodes(uid1, NodeFact, 100, 0)
	if len(nodes1) != 2 {
		t.Errorf("user1 should see 2 nodes, got %d", len(nodes1))
	}

	nodes2, _ := store.ListNodes(uid2, NodeFact, 100, 0)
	if len(nodes2) != 1 {
		t.Errorf("user2 should see 1 node, got %d", len(nodes2))
	}
}
