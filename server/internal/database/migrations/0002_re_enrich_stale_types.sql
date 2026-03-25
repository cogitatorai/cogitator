-- Re-enrich nodes whose type was never persisted by UpdateNode.
-- Before this fix, the enricher reclassified node types in memory but
-- UpdateNode did not include type in its SET clause, so the original
-- type (e.g. "episode") was stuck in the database.
-- Reset enrichment_status to 'pending' so the enricher re-processes
-- them and (with the UpdateNode fix) persists the correct type.
UPDATE nodes SET enrichment_status = 'pending'
WHERE enrichment_status = 'complete';

-- Restore skill nodes that were incorrectly reclassified to "fact" by the
-- enricher (the enricher validation did not include "skill" as a valid type).
-- Skills created via the skills system have origin 'bundled' or 'clawhub'.
UPDATE nodes SET type = 'skill'
WHERE type = 'fact'
  AND (origin = 'bundled' OR origin = 'clawhub');
