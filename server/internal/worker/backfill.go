package worker

import (
	"context"
	"log/slog"

	"github.com/cogitatorai/cogitator/server/internal/bus"
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

// RunReenrichment resets enriched nodes to pending in type-priority order.
// Returns the total number of nodes reset.
func RunReenrichment(ctx context.Context, store *memory.Store, eventBus *bus.Bus, batchSize int) (int, error) {
	total := 0

	for {
		if ctx.Err() != nil {
			return total, ctx.Err()
		}

		rows, err := store.DB().Query(`
			SELECT id FROM nodes
			WHERE enrichment_status = 'complete'
			ORDER BY CASE type
				WHEN 'preference' THEN 1
				WHEN 'fact' THEN 2
				WHEN 'pattern' THEN 3
				ELSE 4
			END, created_at ASC
			LIMIT ?`, batchSize)
		if err != nil {
			return total, err
		}

		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return total, err
			}
			ids = append(ids, id)
		}
		rows.Close()

		if len(ids) == 0 {
			break
		}

		for _, id := range ids {
			_, err := store.DB().Exec(
				"UPDATE nodes SET enrichment_status = 'pending' WHERE id = ?", id)
			if err != nil {
				slog.Warn("re-enrichment reset failed", "node_id", id, "error", err)
				continue
			}
			total++
		}

		if eventBus != nil {
			eventBus.Publish(bus.Event{Type: bus.EnrichmentQueued})
		}
	}

	if total > 0 {
		slog.Info("re-enrichment: nodes reset to pending", "count", total)
	}
	return total, nil
}

// RunContentLengthBackfill populates content_length for nodes that have content but no length.
func RunContentLengthBackfill(ctx context.Context, store *memory.Store, cm *memory.ContentManager) (int, error) {
	if cm == nil {
		return 0, nil
	}

	rows, err := store.DB().Query(
		`SELECT id, content_path FROM nodes WHERE content_path != '' AND content_path IS NOT NULL AND content_length IS NULL`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type entry struct {
		id          string
		contentPath string
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.contentPath); err != nil {
			return 0, err
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	count := 0
	for _, e := range entries {
		if ctx.Err() != nil {
			break
		}
		content, err := cm.Read(e.contentPath)
		if err != nil {
			slog.Warn("content-length backfill: read failed", "node_id", e.id, "error", err)
			continue
		}
		if err := store.UpdateContentLength(e.id, len(content)); err != nil {
			slog.Warn("content-length backfill: update failed", "node_id", e.id, "error", err)
			continue
		}
		count++
	}

	if count > 0 {
		slog.Info("content-length backfill complete", "updated", count, "total_scanned", len(entries))
	}
	return count, nil
}
