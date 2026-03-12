package worker

import (
	"math"
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
