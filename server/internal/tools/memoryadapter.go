package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/memory"
)

// MemoryWriterAdapter adapts memory.Store (and optionally ContentManager)
// to the MemoryWriter interface so the executor can create memory nodes
// without depending on the full memory package API.
type MemoryWriterAdapter struct {
	Store     *memory.Store
	Content   *memory.ContentManager
	Embedder  *memory.NodeEmbedder
	Retriever *memory.Retriever
	EventBus  *bus.Bus
}

func (a *MemoryWriterAdapter) ToggleMemoryPrivacy(nodeID string, private bool, callerID string) error {
	node, err := a.Store.GetNode(nodeID)
	if err != nil {
		return fmt.Errorf("memory not found: %w", err)
	}

	// Authorization: caller must own the memory or it must be shared.
	if node.UserID != nil && *node.UserID != callerID {
		return fmt.Errorf("access denied: you can only toggle your own memories")
	}

	var newUserID *string
	if private {
		newUserID = &callerID
	}

	if err := a.Store.SetNodePrivacy(nodeID, newUserID); err != nil {
		return err
	}

	if a.Retriever != nil {
		a.Retriever.InvalidateCache()
	}
	return nil
}

func (a *MemoryWriterAdapter) SaveMemory(title, content string, pinned bool, userID, subjectID *string) (string, error) {
	if title == "" {
		title = content
		if i := strings.IndexAny(title, ".!?\n"); i > 0 {
			title = title[:i]
		}
		if len(title) > 80 {
			title = title[:80]
		}
	}

	node := &memory.Node{
		Type:             memory.NodeFact,
		Title:            title,
		Summary:          content,
		Confidence:       0.9,
		Origin:           "agent",
		EnrichmentStatus: memory.EnrichmentPending,
		Pinned:           pinned,
		UserID:           userID,
		SubjectID:        subjectID,
	}
	nodeID, err := a.Store.CreateNode(node)
	if err != nil {
		return "", err
	}
	if a.Content != nil && content != "" {
		// Best-effort: write full content for retrieval enrichment.
		a.Content.Write(nodeID, content)
		a.Store.UpdateContentLength(nodeID, len(content))
	}

	// Notify the enricher so edges are generated immediately.
	if a.EventBus != nil {
		a.EventBus.Publish(bus.Event{
			Type:    bus.EnrichmentQueued,
			Payload: map[string]any{"node_id": nodeID},
		})
	}

	// Best-effort embedding at save time.
	if a.Embedder != nil {
		if saved, err := a.Store.GetNode(nodeID); err == nil {
			if err := a.Embedder.EmbedNode(context.Background(), saved); err != nil {
				// Log but don't fail the save.
				_ = err
			}
			if a.Retriever != nil {
				a.Retriever.InvalidateCache()
			}
		}
	}

	return nodeID, nil
}
