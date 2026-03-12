package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/memory"
	"github.com/cogitatorai/cogitator/server/internal/provider"
)

// Consolidator clusters related memory nodes into pattern nodes using
// an adaptive threshold that starts low and scales with the knowledge base.
type Consolidator struct {
	mu       sync.RWMutex
	store    *memory.Store
	provider provider.Provider
	model    string
	eventBus *bus.Bus
	logger   *slog.Logger
	cancel   context.CancelFunc

	minThreshold int
	maxThreshold int
	scale        int
	clusterSim   float64 // minimum cosine similarity to form a cluster (default 0.7)
	minCluster   int     // minimum cluster size to synthesize a pattern (default 3)
}

// ConsolidatorConfig holds dependencies and tuning parameters for the Consolidator.
type ConsolidatorConfig struct {
	Store        *memory.Store
	Provider     provider.Provider
	Model        string
	EventBus     *bus.Bus
	Logger       *slog.Logger
	MinThreshold int // minimum unconsolidated count before triggering (default 5)
	MaxThreshold int // cap on the adaptive threshold (default 50)
	Scale        int // nodes per threshold step: threshold = min + total/scale (default 20)
}

// NewConsolidator creates a Consolidator from the provided configuration.
func NewConsolidator(cfg ConsolidatorConfig) *Consolidator {
	if cfg.MinThreshold <= 0 {
		cfg.MinThreshold = 5
	}
	if cfg.MaxThreshold <= 0 {
		cfg.MaxThreshold = 50
	}
	if cfg.Scale <= 0 {
		cfg.Scale = 20
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Consolidator{
		store:        cfg.Store,
		provider:     cfg.Provider,
		model:        cfg.Model,
		eventBus:     cfg.EventBus,
		logger:       cfg.Logger,
		minThreshold: cfg.MinThreshold,
		maxThreshold: cfg.MaxThreshold,
		scale:        cfg.Scale,
		clusterSim:   0.7,
		minCluster:   3,
	}
}

// SetProvider hot-swaps the LLM provider and model.
func (c *Consolidator) SetProvider(p provider.Provider, model string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.provider = p
	if model != "" {
		c.model = model
	}
}

// adaptiveThreshold computes the consolidation trigger threshold.
// Formula: min(minThreshold + totalNodes/scale, maxThreshold)
func adaptiveThreshold(totalNodes, minThreshold, maxThreshold, scale int) int {
	t := minThreshold + totalNodes/scale
	if t > maxThreshold {
		return maxThreshold
	}
	return t
}

// Start subscribes to EnrichmentQueued events and begins the consolidation loop.
func (c *Consolidator) Start(ctx context.Context) {
	ctx, c.cancel = context.WithCancel(ctx)
	ch := c.eventBus.Subscribe(bus.EnrichmentQueued)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-ch:
				if !ok {
					return
				}
				c.checkAndConsolidate(ctx)
			}
		}
	}()

	c.logger.Info("consolidator started")
}

// Stop cancels the consolidation loop.
func (c *Consolidator) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *Consolidator) checkAndConsolidate(ctx context.Context) {
	c.mu.RLock()
	p := c.provider
	model := c.model
	c.mu.RUnlock()

	if p == nil {
		return
	}

	unconsolidated, err := c.store.GetUnconsolidatedCount()
	if err != nil {
		c.logger.Error("consolidator: count failed", "error", err)
		return
	}

	// Determine total node count for the adaptive threshold calculation.
	stats, _ := c.store.Stats()
	totalNodes := 0
	for _, v := range stats {
		totalNodes += v
	}

	threshold := adaptiveThreshold(totalNodes, c.minThreshold, c.maxThreshold, c.scale)
	if unconsolidated < threshold {
		return
	}

	c.logger.Info("consolidation triggered", "unconsolidated", unconsolidated, "threshold", threshold)
	c.consolidate(ctx, p, model)
}

func (c *Consolidator) consolidate(ctx context.Context, p provider.Provider, model string) {
	// Load enriched, unconsolidated candidate nodes.
	candidates, err := c.store.GetUnconsolidatedNodes(500)
	if err != nil {
		c.logger.Error("consolidator: list nodes failed", "error", err)
		return
	}

	if len(candidates) < c.minCluster {
		return
	}

	allEmb, err := c.store.GetAllEmbeddings("")
	if err != nil {
		c.logger.Error("consolidator: get embeddings failed", "error", err)
		return
	}

	clusters := c.clusterNodes(candidates, allEmb)

	for _, cluster := range clusters {
		if ctx.Err() != nil {
			return
		}
		if len(cluster) < c.minCluster {
			continue
		}
		c.synthesizePattern(ctx, p, model, cluster)
	}
}

// sameOwnership returns true when two nodes belong to the same ownership scope.
// Both nil (shared) counts as the same scope; both non-nil must point to the
// same user ID string.
func sameOwnership(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// clusterNodes groups nodes by embedding similarity using greedy single-linkage.
// A node is added to the first cluster whose seed embedding meets the similarity
// threshold; unembedded nodes are skipped. Nodes with different ownership (UserID)
// are never placed in the same cluster to prevent mixing shared and private data.
func (c *Consolidator) clusterNodes(nodes []memory.Node, embeddings map[string][]float32) [][]memory.Node {
	assigned := make(map[string]int) // node ID to cluster index
	var clusters [][]memory.Node

	for i, node := range nodes {
		if _, ok := assigned[node.ID]; ok {
			continue
		}
		emb, hasEmb := embeddings[node.ID]
		if !hasEmb {
			continue
		}

		clusterIdx := len(clusters)
		cluster := []memory.Node{node}
		assigned[node.ID] = clusterIdx

		for j := i + 1; j < len(nodes); j++ {
			other := nodes[j]
			if _, ok := assigned[other.ID]; ok {
				continue
			}
			if !sameOwnership(node.UserID, other.UserID) {
				continue
			}
			otherEmb, ok := embeddings[other.ID]
			if !ok {
				continue
			}
			if memory.CosineSimilarity(emb, otherEmb) >= c.clusterSim {
				cluster = append(cluster, other)
				assigned[other.ID] = clusterIdx
			}
		}
		clusters = append(clusters, cluster)
	}
	return clusters
}

func (c *Consolidator) synthesizePattern(ctx context.Context, p provider.Provider, model string, cluster []memory.Node) {
	var sb strings.Builder
	sb.WriteString("Synthesize a concise pattern from these related memories:\n")
	for _, n := range cluster {
		sb.WriteString("- ")
		sb.WriteString(n.Title)
		if n.Summary != "" {
			sb.WriteString(": ")
			sb.WriteString(n.Summary)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\nRespond with a JSON object: {\"title\": \"...\", \"description\": \"...\", \"triggers\": [\"...\"]}")

	messages := []provider.Message{
		{Role: "system", Content: "You are a knowledge synthesis agent. You identify patterns from clusters of related observations. Respond ONLY with valid JSON."},
		{Role: "user", Content: sb.String()},
	}

	resp, err := p.Chat(ctx, messages, nil, model, nil)
	if err != nil {
		c.logger.Error("consolidator: LLM synthesis failed", "error", err)
		return
	}

	var result struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Triggers    []string `json:"triggers"`
	}
	raw := strings.TrimSpace(resp.Content)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		c.logger.Error("consolidator: parse synthesis failed", "raw", raw, "error", err)
		return
	}

	if result.Title == "" {
		c.logger.Warn("consolidator: synthesis returned empty title, skipping")
		return
	}

	// Inherit ownership from the cluster. All nodes in a cluster share the
	// same UserID (enforced by clusterNodes), so we use the first node's value.
	var ownerID *string
	if cluster[0].UserID != nil {
		uid := *cluster[0].UserID
		ownerID = &uid
	}

	patternNode := &memory.Node{
		Type:              memory.NodePattern,
		UserID:            ownerID,
		Title:             result.Title,
		Summary:           result.Description,
		RetrievalTriggers: result.Triggers,
		EnrichmentStatus:  memory.EnrichmentComplete,
		Origin:            "consolidation",
	}
	patternID, err := c.store.CreateNode(patternNode)
	if err != nil {
		c.logger.Error("consolidator: create pattern node failed", "error", err)
		return
	}

	for _, n := range cluster {
		if edgeErr := c.store.CreateEdge(&memory.Edge{
			SourceID: patternID,
			TargetID: n.ID,
			UserID:   ownerID,
			Relation: memory.RelDerivedFrom,
			Weight:   1.0,
		}); edgeErr != nil {
			c.logger.Warn("consolidator: create edge failed", "source", patternID, "target", n.ID, "error", edgeErr)
		}
		n.ConsolidatedInto = patternID
		if updateErr := c.store.UpdateNode(&n); updateErr != nil {
			c.logger.Warn("consolidator: update node failed", "node_id", n.ID, "error", updateErr)
		}
	}

	c.logger.Info("pattern synthesized",
		"pattern_id", patternID,
		"title", result.Title,
		"source_count", len(cluster),
	)
}
