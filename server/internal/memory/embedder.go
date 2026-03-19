package memory

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/cogitatorai/cogitator/server/internal/provider"
)

const (
	maxContentLen       = 6000  // max chars of content to include
	maxEmbeddingTextLen = 30000 // final safeguard on total assembled text
)

// NodeEmbedder wraps an Embedder provider and Store to handle the logic
// of building embedding text from a node and persisting the result.
type NodeEmbedder struct {
	mu       sync.RWMutex
	store    *Store
	content  *ContentManager
	embedder provider.Embedder
	model    string
	logger   *slog.Logger
}

func NewNodeEmbedder(store *Store, content *ContentManager, embedder provider.Embedder, model string, logger *slog.Logger) *NodeEmbedder {
	if logger == nil {
		logger = slog.Default()
	}
	return &NodeEmbedder{store: store, content: content, embedder: embedder, model: model, logger: logger}
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

	var nodeContent string
	if node.ContentPath != "" && ne.content != nil {
		if c, err := ne.content.Read(node.ContentPath); err == nil {
			nodeContent = c
		}
	}

	text := buildEmbeddingTextWithContent(node, nodeContent)
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
	return buildEmbeddingTextWithContent(node, "")
}

// buildEmbeddingTextWithContent constructs the text to embed from a node's
// metadata and optional full content, with length safeguards.
func buildEmbeddingTextWithContent(node *Node, content string) string {
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
	if content != "" {
		if len(content) > maxContentLen {
			content = content[:maxContentLen]
		}
		parts = append(parts, content)
	}
	text := strings.Join(parts, " ")
	if len(text) > maxEmbeddingTextLen {
		text = text[:maxEmbeddingTextLen]
	}
	return text
}
