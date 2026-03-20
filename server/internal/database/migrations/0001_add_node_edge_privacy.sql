-- Add private flag to nodes and edges.
ALTER TABLE nodes ADD COLUMN private BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE edges ADD COLUMN private BOOLEAN NOT NULL DEFAULT 0;

-- Add composite indexes for the new visibility query pattern.
CREATE INDEX IF NOT EXISTS idx_nodes_private_user ON nodes(private, user_id);
CREATE INDEX IF NOT EXISTS idx_edges_private_user ON edges(private, user_id);

-- Backfill user_id for existing nodes.
-- Nodes with subject_id pointing to an active user get that user as owner.
-- Nodes with no subject_id or unknown subject_id get assigned to admin (Andrei).
UPDATE nodes SET user_id = COALESCE(
    (SELECT id FROM users WHERE id = nodes.subject_id),
    '384384f7-cd57-488c-8f75-b5f089e3b5f4'
) WHERE user_id IS NULL;

-- Backfill user_id for existing edges.
-- Assign each edge to the source node's owner.
UPDATE edges SET user_id = (
    SELECT user_id FROM nodes WHERE nodes.id = edges.source_id
) WHERE edges.user_id IS NULL;
