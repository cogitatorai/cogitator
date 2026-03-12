package worker

import (
	"context"
	"log/slog"

	"github.com/cogitatorai/cogitator/server/internal/memory"
)

// RunBackfill scans for nodes without embeddings and generates them in batches.
// Returns the number of nodes backfilled.
func RunBackfill(ctx context.Context, store *memory.Store, embedder *memory.NodeEmbedder, batchSize int) (int, error) {
	if embedder == nil {
		return 0, nil
	}

	nodes, err := store.GetNodesWithoutEmbeddings(batchSize)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, node := range nodes {
		if ctx.Err() != nil {
			break
		}
		n := node // copy for pointer
		if err := embedder.EmbedNode(ctx, &n); err != nil {
			slog.Warn("backfill embed failed", "node_id", node.ID, "error", err)
			continue
		}
		count++
	}

	if count > 0 {
		slog.Info("backfill complete", "embedded", count, "total_scanned", len(nodes))
	}
	return count, nil
}
