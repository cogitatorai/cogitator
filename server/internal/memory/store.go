package memory

import (
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/database"
	"github.com/oklog/ulid/v2"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db *database.DB
}

func NewStore(db *database.DB) *Store {
	return &Store{db: db}
}

func newID() string {
	return ulid.Make().String()
}

func (s *Store) CreateNode(n *Node) (string, error) {
	n.ID = newID()
	now := time.Now()
	n.CreatedAt = now
	n.UpdatedAt = now
	if n.EnrichmentStatus == "" {
		n.EnrichmentStatus = EnrichmentPending
	}

	tags, _ := json.Marshal(n.Tags)
	triggers, _ := json.Marshal(n.RetrievalTriggers)

	_, err := s.db.Exec(`INSERT INTO nodes
		(id, type, title, summary, tags, retrieval_triggers, confidence,
		 content_path, enrichment_status, origin, source_url, version, skill_path,
		 created_at, updated_at, pinned, consolidated_into, user_id, subject_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, n.Type, n.Title, n.Summary, string(tags), string(triggers),
		n.Confidence, n.ContentPath, n.EnrichmentStatus, n.Origin,
		n.SourceURL, n.Version, n.SkillPath, n.CreatedAt, n.UpdatedAt,
		n.Pinned, n.ConsolidatedInto, n.UserID, n.SubjectID)
	if err != nil {
		return "", err
	}
	return n.ID, nil
}

func (s *Store) GetNode(id string) (*Node, error) {
	var n Node
	var summary, tags, triggers, contentPath, origin, sourceURL, version, skillPath, consolidatedInto sql.NullString
	var userID, subjectID sql.NullString
	var lastAccessed sql.NullTime

	err := s.db.QueryRow(`SELECT
		id, type, title, summary, tags, retrieval_triggers, confidence,
		content_path, enrichment_status, origin, source_url, version, skill_path,
		created_at, updated_at, last_accessed, pinned, consolidated_into, user_id, subject_id
		FROM nodes WHERE id = ?`, id).Scan(
		&n.ID, &n.Type, &n.Title, &summary, &tags, &triggers,
		&n.Confidence, &contentPath, &n.EnrichmentStatus, &origin,
		&sourceURL, &version, &skillPath,
		&n.CreatedAt, &n.UpdatedAt, &lastAccessed, &n.Pinned, &consolidatedInto, &userID, &subjectID)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	n.Summary = summary.String
	n.ContentPath = contentPath.String
	n.Origin = origin.String
	n.SourceURL = sourceURL.String
	n.Version = version.String
	n.SkillPath = skillPath.String
	n.ConsolidatedInto = consolidatedInto.String

	if userID.Valid {
		n.UserID = &userID.String
	}
	if subjectID.Valid {
		n.SubjectID = &subjectID.String
	}
	if tags.Valid {
		json.Unmarshal([]byte(tags.String), &n.Tags)
	}
	if triggers.Valid {
		json.Unmarshal([]byte(triggers.String), &n.RetrievalTriggers)
	}
	if lastAccessed.Valid {
		n.LastAccessed = &lastAccessed.Time
	}
	return &n, nil
}

// SetNodePrivacy updates the user_id of a node to control its visibility.
// Pass nil to make the node shared; pass a user ID to make it private.
func (s *Store) SetNodePrivacy(id string, userID *string) error {
	res, err := s.db.Exec("UPDATE nodes SET user_id = ?, updated_at = ? WHERE id = ?",
		userID, time.Now(), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// FindNodeBySkillPath returns the node with the given skill_path, or nil if none exists.
func (s *Store) FindNodeBySkillPath(skillPath string) (*Node, error) {
	var id string
	err := s.db.QueryRow("SELECT id FROM nodes WHERE skill_path = ? LIMIT 1", skillPath).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.GetNode(id)
}

func (s *Store) UpdateNode(n *Node) error {
	n.UpdatedAt = time.Now()
	tags, _ := json.Marshal(n.Tags)
	triggers, _ := json.Marshal(n.RetrievalTriggers)

	_, err := s.db.Exec(`UPDATE nodes SET
		title=?, summary=?, tags=?, retrieval_triggers=?, confidence=?,
		content_path=?, enrichment_status=?, origin=?, source_url=?,
		version=?, skill_path=?, updated_at=?, pinned=?, consolidated_into=?
		WHERE id=?`,
		n.Title, n.Summary, string(tags), string(triggers), n.Confidence,
		n.ContentPath, n.EnrichmentStatus, n.Origin, n.SourceURL,
		n.Version, n.SkillPath, n.UpdatedAt, n.Pinned, n.ConsolidatedInto, n.ID)
	return err
}

func (s *Store) DeleteNode(id string) error {
	_, err := s.db.Exec("DELETE FROM nodes WHERE id = ?", id)
	return err
}

func (s *Store) ListNodes(userID string, nodeType NodeType, limit, offset int) ([]Node, error) {
	query := `SELECT
		id, type, title, summary, confidence, enrichment_status,
		origin, source_url, version, skill_path, created_at, updated_at,
		pinned, consolidated_into, user_id, subject_id
		FROM nodes WHERE 1=1`
	var args []any

	if userID != "" {
		query += " AND (user_id IS NULL OR user_id = ?)"
		args = append(args, userID)
		// Hide memories about other people from the current user's list.
		query += " AND (subject_id IS NULL OR subject_id = ?)"
		args = append(args, userID)
	}
	if nodeType != "" {
		query += " AND type = ?"
		args = append(args, nodeType)
	}
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		var summary, origin, sourceURL, version, skillPath, consolidatedInto sql.NullString
		var uid, sid sql.NullString
		if err := rows.Scan(&n.ID, &n.Type, &n.Title, &summary,
			&n.Confidence, &n.EnrichmentStatus,
			&origin, &sourceURL, &version, &skillPath,
			&n.CreatedAt, &n.UpdatedAt, &n.Pinned, &consolidatedInto, &uid, &sid); err != nil {
			return nil, err
		}
		n.Summary = summary.String
		n.Origin = origin.String
		n.SourceURL = sourceURL.String
		n.Version = version.String
		n.SkillPath = skillPath.String
		n.ConsolidatedInto = consolidatedInto.String
		if uid.Valid {
			n.UserID = &uid.String
		}
		if sid.Valid {
			n.SubjectID = &sid.String
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

func (s *Store) GetPendingEnrichment(limit int) ([]Node, error) {
	rows, err := s.db.Query(`SELECT
		id, type, title, summary, tags, retrieval_triggers, confidence,
		content_path, enrichment_status, origin, source_url, version, skill_path,
		created_at, updated_at, last_accessed, pinned, consolidated_into, user_id, subject_id
		FROM nodes WHERE enrichment_status = 'pending'
		ORDER BY created_at ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanNodes(rows)
}

func (s *Store) GetNodeSummaries(userID string, types ...NodeType) ([]NodeSummary, error) {
	query := "SELECT id, type, title, summary, retrieval_triggers FROM nodes WHERE 1=1"
	var args []any

	if userID != "" {
		query += " AND (user_id IS NULL OR user_id = ?)"
		args = append(args, userID)
	}
	if len(types) > 0 {
		placeholders := ""
		for i, t := range types {
			if i > 0 {
				placeholders += ","
			}
			placeholders += "?"
			args = append(args, t)
		}
		query += " AND type IN (" + placeholders + ")"
	}
	query += " ORDER BY confidence DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []NodeSummary
	for rows.Next() {
		var ns NodeSummary
		var summary, triggers sql.NullString
		if err := rows.Scan(&ns.ID, &ns.Type, &ns.Title, &summary, &triggers); err != nil {
			return nil, err
		}
		ns.Summary = summary.String
		if triggers.Valid {
			json.Unmarshal([]byte(triggers.String), &ns.RetrievalTriggers)
		}
		summaries = append(summaries, ns)
	}
	return summaries, rows.Err()
}

func (s *Store) CreateEdge(e *Edge) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	_, err := s.db.Exec(`INSERT INTO edges (source_id, target_id, relation, weight, created_at, user_id)
		VALUES (?, ?, ?, ?, ?, ?)`,
		e.SourceID, e.TargetID, e.Relation, e.Weight, e.CreatedAt, e.UserID)
	return err
}

func (s *Store) GetEdgesFrom(nodeID, userID string) ([]Edge, error) {
	query := `SELECT e.source_id, e.target_id, e.relation, e.weight, e.created_at, e.user_id
		FROM edges e`
	args := []any{nodeID}
	if userID != "" {
		query += ` JOIN nodes target ON e.target_id = target.id
			WHERE e.source_id = ? AND (target.user_id IS NULL OR target.user_id = ?)`
		args = append(args, userID)
	} else {
		query += ` WHERE e.source_id = ?`
	}
	return s.queryEdges(query, args...)
}

func (s *Store) GetEdgesTo(nodeID, userID string) ([]Edge, error) {
	query := `SELECT e.source_id, e.target_id, e.relation, e.weight, e.created_at, e.user_id
		FROM edges e`
	args := []any{nodeID}
	if userID != "" {
		query += ` JOIN nodes source ON e.source_id = source.id
			WHERE e.target_id = ? AND (source.user_id IS NULL OR source.user_id = ?)`
		args = append(args, userID)
	} else {
		query += ` WHERE e.target_id = ?`
	}
	return s.queryEdges(query, args...)
}

func (s *Store) GetConnectedNodes(nodeID string) ([]NodeSummary, error) {
	rows, err := s.db.Query(`
		SELECT DISTINCT n.id, n.type, n.title, n.summary, n.retrieval_triggers
		FROM nodes n
		JOIN edges e ON (e.target_id = n.id AND e.source_id = ?) OR (e.source_id = n.id AND e.target_id = ?)
		ORDER BY e.weight DESC`, nodeID, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []NodeSummary
	for rows.Next() {
		var ns NodeSummary
		var summary, triggers sql.NullString
		if err := rows.Scan(&ns.ID, &ns.Type, &ns.Title, &summary, &triggers); err != nil {
			return nil, err
		}
		ns.Summary = summary.String
		if triggers.Valid {
			json.Unmarshal([]byte(triggers.String), &ns.RetrievalTriggers)
		}
		summaries = append(summaries, ns)
	}
	return summaries, rows.Err()
}

// GetConnectedNodesByUser returns nodes connected to nodeID, filtered by
// user visibility (shared + caller's private nodes).
func (s *Store) GetConnectedNodesByUser(nodeID, userID string) ([]NodeSummary, error) {
	query := `
		SELECT DISTINCT n.id, n.type, n.title, n.summary, n.retrieval_triggers
		FROM nodes n
		JOIN edges e ON (e.target_id = n.id AND e.source_id = ?) OR (e.source_id = n.id AND e.target_id = ?)`
	args := []any{nodeID, nodeID}
	if userID != "" {
		query += ` WHERE (n.user_id IS NULL OR n.user_id = ?)`
		args = append(args, userID)
	}
	query += ` ORDER BY e.weight DESC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []NodeSummary
	for rows.Next() {
		var ns NodeSummary
		var summary, triggers sql.NullString
		if err := rows.Scan(&ns.ID, &ns.Type, &ns.Title, &summary, &triggers); err != nil {
			return nil, err
		}
		ns.Summary = summary.String
		if triggers.Valid {
			json.Unmarshal([]byte(triggers.String), &ns.RetrievalTriggers)
		}
		summaries = append(summaries, ns)
	}
	return summaries, rows.Err()
}

// GetVisibleEdges returns all edges visible to the given user.
// When userID is empty, returns all edges (no filtering).
func (s *Store) GetVisibleEdges(userID string) ([]Edge, error) {
	if userID == "" {
		return s.ListAllEdges()
	}
	return s.queryEdges(`SELECT e.source_id, e.target_id, e.relation, e.weight, e.created_at, e.user_id
		FROM edges e
		WHERE e.user_id IS NULL OR e.user_id = ?`, userID)
}

func (s *Store) DeleteEdge(sourceID, targetID string, relation RelationType) error {
	_, err := s.db.Exec("DELETE FROM edges WHERE source_id = ? AND target_id = ? AND relation = ?",
		sourceID, targetID, relation)
	return err
}

func (s *Store) TouchAccess(nodeID string) error {
	_, err := s.db.Exec("UPDATE nodes SET last_accessed = ? WHERE id = ?", time.Now(), nodeID)
	return err
}

func (s *Store) Stats() (map[string]int, error) {
	stats := make(map[string]int)

	var total int
	s.db.QueryRow("SELECT COUNT(*) FROM nodes").Scan(&total)
	stats["total_nodes"] = total

	s.db.QueryRow("SELECT COUNT(*) FROM edges").Scan(&total)
	stats["total_edges"] = total

	s.db.QueryRow("SELECT COUNT(*) FROM nodes WHERE enrichment_status = 'pending'").Scan(&total)
	stats["pending_enrichment"] = total

	return stats, nil
}

func (s *Store) queryEdges(query string, args ...any) ([]Edge, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []Edge
	for rows.Next() {
		var e Edge
		var uid sql.NullString
		if err := rows.Scan(&e.SourceID, &e.TargetID, &e.Relation, &e.Weight, &e.CreatedAt, &uid); err != nil {
			return nil, err
		}
		if uid.Valid {
			e.UserID = &uid.String
		}
		edges = append(edges, e)
	}
	return edges, rows.Err()
}

// ListAllEdges returns every edge in the graph.
func (s *Store) ListAllEdges() ([]Edge, error) {
	return s.queryEdges("SELECT source_id, target_id, relation, weight, created_at, user_id FROM edges")
}

// ListAllNodeSummaries returns summaries for all nodes regardless of type.
func (s *Store) ListAllNodeSummaries() ([]NodeSummary, error) {
	return s.GetNodeSummaries("")
}

// SaveEmbedding stores (or replaces) the embedding vector for a node.
// The vector is stored as raw little-endian float32 bytes.
func (s *Store) SaveEmbedding(nodeID string, vec []float32, model string) error {
	blob := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(blob[i*4:], math.Float32bits(v))
	}
	_, err := s.db.Exec(`INSERT OR REPLACE INTO node_embeddings (node_id, embedding, model, dimensions, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)`, nodeID, blob, model, len(vec))
	return err
}

// GetEmbedding returns the embedding vector for a node, or nil if none exists.
func (s *Store) GetEmbedding(nodeID string) ([]float32, error) {
	var blob []byte
	err := s.db.QueryRow("SELECT embedding FROM node_embeddings WHERE node_id = ?", nodeID).Scan(&blob)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return blobToVec(blob), nil
}

// GetAllEmbeddings returns all (node_id, embedding) pairs for the in-memory cache.
// When userID is non-empty, only embeddings for visible nodes (shared + own) are returned.
func (s *Store) GetAllEmbeddings(userID string) (map[string][]float32, error) {
	query := `SELECT ne.node_id, ne.embedding FROM node_embeddings ne`
	var args []any
	if userID != "" {
		query += ` JOIN nodes n ON ne.node_id = n.id
			WHERE n.user_id IS NULL OR n.user_id = ?`
		args = append(args, userID)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]float32)
	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, err
		}
		result[id] = blobToVec(blob)
	}
	return result, rows.Err()
}

// DeleteEmbedding removes the embedding for a node.
func (s *Store) DeleteEmbedding(nodeID string) error {
	_, err := s.db.Exec("DELETE FROM node_embeddings WHERE node_id = ?", nodeID)
	return err
}

// DeleteAllEmbeddings removes all embedding rows. Returns the number deleted.
// Used when the embedding model changes and all vectors must be regenerated.
func (s *Store) DeleteAllEmbeddings() (int64, error) {
	res, err := s.db.Exec("DELETE FROM node_embeddings")
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func blobToVec(blob []byte) []float32 {
	n := len(blob) / 4
	vec := make([]float32, n)
	for i := 0; i < n; i++ {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4:]))
	}
	return vec
}

// GetPinnedNodes returns all nodes with pinned=1.
// When userID is non-empty, only visible nodes (shared + own) are returned.
func (s *Store) GetPinnedNodes(userID string) ([]Node, error) {
	query := `SELECT
		id, type, title, summary, tags, retrieval_triggers, confidence,
		content_path, enrichment_status, origin, source_url, version, skill_path,
		created_at, updated_at, last_accessed, pinned, consolidated_into, user_id, subject_id
		FROM nodes WHERE pinned = 1`
	var args []any
	if userID != "" {
		query += " AND (user_id IS NULL OR user_id = ?)"
		args = append(args, userID)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// GetUnconsolidatedNodes returns enriched nodes that have not been consolidated yet, up to limit.
func (s *Store) GetUnconsolidatedNodes(limit int) ([]Node, error) {
	rows, err := s.db.Query(`SELECT
		id, type, title, summary, tags, retrieval_triggers, confidence,
		content_path, enrichment_status, origin, source_url, version, skill_path,
		created_at, updated_at, last_accessed, pinned, consolidated_into, user_id, subject_id
		FROM nodes
		WHERE enrichment_status = 'complete' AND (consolidated_into IS NULL OR consolidated_into = '')
		ORDER BY created_at ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// GetUnconsolidatedCount returns the number of enriched nodes not yet consolidated.
func (s *Store) GetUnconsolidatedCount() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM nodes
		WHERE enrichment_status = 'complete' AND (consolidated_into IS NULL OR consolidated_into = '')`).Scan(&count)
	return count, err
}

// GetNodesWithoutEmbeddings returns nodes that have no embedding row, up to limit.
func (s *Store) GetNodesWithoutEmbeddings(limit int) ([]Node, error) {
	rows, err := s.db.Query(`SELECT n.id, n.type, n.title, n.summary, n.tags, n.retrieval_triggers,
		n.confidence, n.content_path, n.enrichment_status, n.origin, n.source_url, n.version, n.skill_path,
		n.created_at, n.updated_at, n.last_accessed, n.pinned, n.consolidated_into, n.user_id, n.subject_id
		FROM nodes n LEFT JOIN node_embeddings ne ON n.id = ne.node_id
		WHERE ne.node_id IS NULL LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// scanNodes reads the full node row set from a query that selects:
// id, type, title, summary, tags, retrieval_triggers, confidence,
// content_path, enrichment_status, origin, source_url, version, skill_path,
// created_at, updated_at, last_accessed, pinned, consolidated_into, user_id, subject_id
func scanNodes(rows *sql.Rows) ([]Node, error) {
	var nodes []Node
	for rows.Next() {
		var n Node
		var summary, tags, triggers, contentPath, origin, sourceURL, version, skillPath, consolidatedInto sql.NullString
		var userID, subjectID sql.NullString
		var lastAccessed sql.NullTime
		if err := rows.Scan(&n.ID, &n.Type, &n.Title, &summary, &tags, &triggers,
			&n.Confidence, &contentPath, &n.EnrichmentStatus, &origin,
			&sourceURL, &version, &skillPath,
			&n.CreatedAt, &n.UpdatedAt, &lastAccessed, &n.Pinned, &consolidatedInto, &userID, &subjectID); err != nil {
			return nil, err
		}
		n.Summary = summary.String
		n.ContentPath = contentPath.String
		n.Origin = origin.String
		n.SourceURL = sourceURL.String
		n.Version = version.String
		n.SkillPath = skillPath.String
		n.ConsolidatedInto = consolidatedInto.String
		if userID.Valid {
			n.UserID = &userID.String
		}
		if subjectID.Valid {
			n.SubjectID = &subjectID.String
		}
		if tags.Valid {
			json.Unmarshal([]byte(tags.String), &n.Tags)
		}
		if triggers.Valid {
			json.Unmarshal([]byte(triggers.String), &n.RetrievalTriggers)
		}
		if lastAccessed.Valid {
			n.LastAccessed = &lastAccessed.Time
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}
