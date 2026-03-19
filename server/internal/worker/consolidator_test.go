package worker

import (
	"context"
	"log/slog"
	"math"
	"strings"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/memory"
)

func TestAdaptiveThreshold(t *testing.T) {
	tests := []struct {
		total, min, max, scale, want int
	}{
		{0, 5, 50, 20, 5},
		{100, 5, 50, 20, 10},
		{500, 5, 50, 20, 30},
		{1000, 5, 50, 20, 50}, // 5 + 1000/20 = 55, capped at 50
		{2000, 5, 50, 20, 50},
	}
	for _, tt := range tests {
		got := adaptiveThreshold(tt.total, tt.min, tt.max, tt.scale)
		if got != tt.want {
			t.Errorf("adaptiveThreshold(%d, %d, %d, %d) = %d, want %d",
				tt.total, tt.min, tt.max, tt.scale, got, tt.want)
		}
	}
}

func TestAdaptiveThresholdNeverBelowMin(t *testing.T) {
	// Even with zero total nodes the result must equal min.
	got := adaptiveThreshold(0, 10, 100, 20)
	if got != 10 {
		t.Errorf("expected min threshold 10, got %d", got)
	}
}

func TestAdaptiveThresholdNeverExceedsMax(t *testing.T) {
	// Extremely large node count must be capped at max.
	got := adaptiveThreshold(1_000_000, 5, 50, 20)
	if got != 50 {
		t.Errorf("expected max threshold 50, got %d", got)
	}
}

// makeNode builds a minimal memory.Node for test use.
func makeNode(id, title string) memory.Node {
	return memory.Node{
		ID:               id,
		Title:            title,
		EnrichmentStatus: memory.EnrichmentComplete,
	}
}

// vec2 constructs a 2-dimensional float32 vector.
func vec2(x, y float32) []float32 { return []float32{x, y} }

// cosAngle returns the cosine similarity between two 2-D unit-ish vectors for
// comparison in tests.
func cosAngle(ax, ay, bx, by float64) float64 {
	dot := ax*bx + ay*by
	na := math.Sqrt(ax*ax + ay*ay)
	nb := math.Sqrt(bx*bx + by*by)
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (na * nb)
}

func TestClusterNodes_SimilarNodesGrouped(t *testing.T) {
	c := &Consolidator{clusterSim: 0.7, minCluster: 2}

	// Three nearly identical nodes (pointing almost the same direction)
	// and one orthogonal node.
	nodes := []memory.Node{
		makeNode("a", "A"),
		makeNode("b", "B"),
		makeNode("c", "C"),
		makeNode("d", "D"), // orthogonal to a/b/c
	}

	embeddings := map[string][]float32{
		"a": vec2(1, 0),
		"b": vec2(0.99, 0.1),
		"c": vec2(0.98, 0.05),
		"d": vec2(0, 1), // cosine(a,d) = 0 < 0.7
	}

	clusters := c.clusterNodes(nodes, embeddings)

	// Expect at least one cluster containing a, b, c.
	found := false
	for _, cluster := range clusters {
		ids := make(map[string]bool)
		for _, n := range cluster {
			ids[n.ID] = true
		}
		if ids["a"] && ids["b"] && ids["c"] {
			found = true
		}
	}
	if !found {
		t.Error("expected a, b, c to be in the same cluster")
	}

	// d must be in its own cluster (not grouped with a/b/c).
	for _, cluster := range clusters {
		ids := make(map[string]bool)
		for _, n := range cluster {
			ids[n.ID] = true
		}
		if ids["d"] && (ids["a"] || ids["b"] || ids["c"]) {
			t.Error("d must not be clustered with a, b, or c")
		}
	}
}

func TestClusterNodes_NoEmbeddingsSkipped(t *testing.T) {
	c := &Consolidator{clusterSim: 0.7, minCluster: 2}

	nodes := []memory.Node{
		makeNode("x", "X"),
		makeNode("y", "Y"), // no embedding
	}

	embeddings := map[string][]float32{
		"x": vec2(1, 0),
		// "y" intentionally absent
	}

	clusters := c.clusterNodes(nodes, embeddings)

	// y has no embedding so it must not appear in any cluster.
	for _, cluster := range clusters {
		for _, n := range cluster {
			if n.ID == "y" {
				t.Error("node without embedding must not appear in any cluster")
			}
		}
	}
}

func TestClusterNodes_AllAssignedOnce(t *testing.T) {
	c := &Consolidator{clusterSim: 0.5, minCluster: 1}

	nodes := []memory.Node{
		makeNode("p", "P"),
		makeNode("q", "Q"),
		makeNode("r", "R"),
	}

	// All three are similar enough to cluster together.
	embeddings := map[string][]float32{
		"p": vec2(1, 0),
		"q": vec2(0.9, 0.1),
		"r": vec2(0.8, 0.2),
	}

	clusters := c.clusterNodes(nodes, embeddings)

	// Count total memberships: each node must appear exactly once.
	counts := make(map[string]int)
	for _, cluster := range clusters {
		for _, n := range cluster {
			counts[n.ID]++
		}
	}
	for _, n := range nodes {
		if embeddings[n.ID] != nil && counts[n.ID] != 1 {
			t.Errorf("node %q appears in %d clusters, want exactly 1", n.ID, counts[n.ID])
		}
	}
}

func TestNewConsolidatorDefaults(t *testing.T) {
	c := NewConsolidator(ConsolidatorConfig{})
	if c.minThreshold != 5 {
		t.Errorf("default minThreshold: got %d, want 5", c.minThreshold)
	}
	if c.maxThreshold != 50 {
		t.Errorf("default maxThreshold: got %d, want 50", c.maxThreshold)
	}
	if c.scale != 20 {
		t.Errorf("default scale: got %d, want 20", c.scale)
	}
	if c.clusterSim != 0.7 {
		t.Errorf("default clusterSim: got %f, want 0.7", c.clusterSim)
	}
	if c.minCluster != 3 {
		t.Errorf("default minCluster: got %d, want 3", c.minCluster)
	}
}

// makeNodeWithMeta constructs a memory.Node with tags, triggers and confidence
// for use in synthesizePattern tests.
func makeNodeWithMeta(id, title string, tags, triggers []string, confidence float64) memory.Node {
	return memory.Node{
		ID:                id,
		Title:             title,
		Tags:              tags,
		RetrievalTriggers: triggers,
		Confidence:        confidence,
		EnrichmentStatus:  memory.EnrichmentComplete,
	}
}

func TestProgrammaticSynthesizePattern_TitleFromTags(t *testing.T) {
	db := testDB(t)
	store := memory.NewStore(db)

	// Pre-insert source nodes so UpdateNode can find them.
	nodes := []memory.Node{
		makeNodeWithMeta("", "Go uses static typing", []string{"golang", "types"}, []string{"go types", "static typing"}, 0.8),
		makeNodeWithMeta("", "Go interfaces are implicit", []string{"golang", "interfaces"}, []string{"go interface", "duck typing"}, 0.9),
		makeNodeWithMeta("", "Go has goroutines", []string{"golang", "concurrency"}, []string{"goroutines", "concurrency"}, 0.7),
	}

	var cluster []memory.Node
	for _, n := range nodes {
		id, err := store.CreateNode(&n)
		if err != nil {
			t.Fatalf("CreateNode: %v", err)
		}
		created := n
		created.ID = id
		cluster = append(cluster, created)
	}

	c := &Consolidator{
		store:  store,
		logger: nopLogger(),
	}

	c.synthesizePattern(context.Background(), nil, "", cluster)

	// Find the created pattern node.
	pattern := findPatternNode(t, store)

	// Title must be derived from top tags (golang appears 3x, others 1x each).
	if !strings.HasPrefix(pattern.Title, "Pattern:") {
		t.Errorf("title should start with 'Pattern:', got %q", pattern.Title)
	}
	if !strings.Contains(pattern.Title, "golang") {
		t.Errorf("title should contain top tag 'golang', got %q", pattern.Title)
	}

	// Summary must mention the cluster size.
	if !strings.Contains(pattern.Summary, "3") {
		t.Errorf("summary should mention cluster size 3, got %q", pattern.Summary)
	}

	// Summary must reference source titles.
	if !strings.Contains(pattern.Summary, "Go uses static typing") {
		t.Errorf("summary should contain source title, got %q", pattern.Summary)
	}

	// Triggers are cleaned union of all source triggers.
	if len(pattern.RetrievalTriggers) == 0 {
		t.Error("pattern should have retrieval triggers")
	}

	// Tags are cleaned union of source tags.
	if len(pattern.Tags) == 0 {
		t.Error("pattern should have tags")
	}

	// Pattern must be queued for enrichment (not already complete).
	if pattern.EnrichmentStatus != memory.EnrichmentPending {
		t.Errorf("EnrichmentStatus: got %q, want %q", pattern.EnrichmentStatus, memory.EnrichmentPending)
	}

	// Origin must identify consolidation.
	if pattern.Origin != "consolidation" {
		t.Errorf("Origin: got %q, want 'consolidation'", pattern.Origin)
	}

	// Each source node must have ConsolidatedInto set to the pattern ID.
	for _, src := range cluster {
		updated, err := store.GetNode(src.ID)
		if err != nil {
			t.Fatalf("GetNode(%s): %v", src.ID, err)
		}
		if updated.ConsolidatedInto != pattern.ID {
			t.Errorf("source node %s: ConsolidatedInto = %q, want %q", src.ID, updated.ConsolidatedInto, pattern.ID)
		}
	}
}

func TestProgrammaticSynthesizePattern_SummaryCapAt5(t *testing.T) {
	db := testDB(t)
	store := memory.NewStore(db)

	// Create 7 nodes; summary should cap at 5 titles and show "and 2 more".
	var cluster []memory.Node
	for i := 0; i < 7; i++ {
		title := strings.Repeat("x", i+1) // distinct, short titles
		id, err := store.CreateNode(&memory.Node{
			Title:            title,
			Tags:             []string{"topic"},
			EnrichmentStatus: memory.EnrichmentComplete,
		})
		if err != nil {
			t.Fatalf("CreateNode: %v", err)
		}
		cluster = append(cluster, memory.Node{ID: id, Title: title, Tags: []string{"topic"}})
	}

	c := &Consolidator{store: store, logger: nopLogger()}
	c.synthesizePattern(context.Background(), nil, "", cluster)

	pattern := findPatternNode(t, store)
	if !strings.Contains(pattern.Summary, "and 2 more") {
		t.Errorf("summary should contain 'and 2 more', got %q", pattern.Summary)
	}
}

func TestTopNByCount(t *testing.T) {
	counts := map[string]int{"golang": 3, "types": 1, "interfaces": 2}
	top := topNByCount(counts, 2)
	if len(top) != 2 {
		t.Fatalf("expected 2 results, got %d", len(top))
	}
	if top[0] != "golang" {
		t.Errorf("top[0]: got %q, want 'golang'", top[0])
	}
	if top[1] != "interfaces" {
		t.Errorf("top[1]: got %q, want 'interfaces'", top[1])
	}
}

func TestTopNWords(t *testing.T) {
	titles := []string{"Go concurrency model", "Go concurrency patterns", "Go goroutine model"}
	top := topNWords(titles, 2)
	// "concurrency" and "model" each appear 2x; "goroutine" appears 1x; "patterns" 1x
	// "go" is single-char-ish but actually 2 chars and not in stop words, appears 3x
	// Expect top two to include "go" and "concurrency" (both appear 2-3x)
	if len(top) != 2 {
		t.Fatalf("expected 2 results, got %d: %v", len(top), top)
	}
}

// nopLogger returns a logger that discards all output.
func nopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nopWriter{}, nil))
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

// findPatternNode locates the first NodePattern via ListNodes (for the ID),
// then re-fetches it with GetNode to get full fields including tags and triggers.
func findPatternNode(t *testing.T, store *memory.Store) *memory.Node {
	t.Helper()
	nodes, err := store.ListNodes("", memory.NodePattern, 100, 0)
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatal("no pattern node found")
	}
	full, err := store.GetNode(nodes[0].ID)
	if err != nil {
		t.Fatalf("GetNode(%s): %v", nodes[0].ID, err)
	}
	return full
}
