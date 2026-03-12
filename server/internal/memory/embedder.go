package memory

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/cogitatorai/cogitator/server/internal/provider"
)

// NodeEmbedder wraps an Embedder provider and Store to handle the logic
// of building embedding text from a node and persisting the result.
type NodeEmbedder struct {
	mu       sync.RWMutex
	store    *Store
	embedder provider.Embedder
	model    string
	logger   *slog.Logger
}

func NewNodeEmbedder(store *Store, embedder provider.Embedder, model string, logger *slog.Logger) *NodeEmbedder {
	if logger == nil {
		logger = slog.Default()
	}
	return &NodeEmbedder{store: store, embedder: embedder, model: model, logger: logger}
}

// SetEmbedder hot-swaps the embedding provider and model.
func (ne *NodeEmbedder) SetEmbedder(e provider.Embedder, model string) {
	ne.mu.Lock()
	defer ne.mu.Unlock()
	ne.embedder = e
	if model != "" {
		ne.model = model
	}
}

// EmbedNode generates and stores an embedding for the given node.
// Returns nil if no embedder is configured (graceful degradation).
func (ne *NodeEmbedder) EmbedNode(ctx context.Context, node *Node) error {
	ne.mu.RLock()
	e := ne.embedder
	model := ne.model
	ne.mu.RUnlock()

	if e == nil {
		return nil
	}

	text := buildEmbeddingText(node)
	vecs, err := e.Embed(ctx, []string{text}, model)
	if err != nil {
		return err
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return nil
	}

	return ne.store.SaveEmbedding(node.ID, vecs[0], model)
}

// buildEmbeddingText constructs the text to embed from a node's metadata.
func buildEmbeddingText(node *Node) string {
	var parts []string
	parts = append(parts, node.Title)
	if node.Summary != "" {
		parts = append(parts, node.Summary)
	}
	if len(node.Tags) > 0 {
		parts = append(parts, strings.Join(node.Tags, " "))
	}
	if len(node.RetrievalTriggers) > 0 {
		parts = append(parts, strings.Join(node.RetrievalTriggers, " "))
	}
	return strings.Join(parts, " ")
}
