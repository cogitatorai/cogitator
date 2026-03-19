package memory

import "time"

type NodeType string

const (
	NodeFact          NodeType = "fact"
	NodePreference    NodeType = "preference"
	NodePattern       NodeType = "pattern"
	NodeSkill         NodeType = "skill"
	NodeEpisode       NodeType = "episode"
	NodeTaskKnowledge NodeType = "task_knowledge"
)

type EnrichmentStatus string

const (
	EnrichmentPending  EnrichmentStatus = "pending"
	EnrichmentComplete EnrichmentStatus = "complete"
)

type Node struct {
	ID                string           `json:"id"`
	UserID            *string          `json:"user_id,omitempty"`
	SubjectID         *string          `json:"subject_id,omitempty"`
	Type              NodeType         `json:"type"`
	Title             string           `json:"title"`
	Summary           string           `json:"summary,omitempty"`
	Tags              []string         `json:"tags,omitempty"`
	RetrievalTriggers []string         `json:"retrieval_triggers,omitempty"`
	Confidence        float64          `json:"confidence"`
	ContentPath       string           `json:"content_path,omitempty"`
	EnrichmentStatus  EnrichmentStatus `json:"enrichment_status"`
	Origin            string           `json:"origin,omitempty"`
	SourceURL         string           `json:"source_url,omitempty"`
	Version           string           `json:"version,omitempty"`
	SkillPath         string           `json:"skill_path,omitempty"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
	LastAccessed      *time.Time       `json:"last_accessed,omitempty"`
	Pinned           bool             `json:"pinned"`
	Private          bool             `json:"private"`
	ConsolidatedInto string           `json:"consolidated_into,omitempty"`
}

type RelationType string

const (
	RelRefines     RelationType = "refines"
	RelContradicts RelationType = "contradicts"
	RelSupports    RelationType = "supports"
	RelDerivedFrom RelationType = "derived_from"
	RelExampleOf   RelationType = "example_of"
	RelRelatedTo   RelationType = "related_to"
)

type Edge struct {
	SourceID  string       `json:"source_id"`
	TargetID  string       `json:"target_id"`
	UserID    *string      `json:"user_id,omitempty"`
	Private   bool         `json:"private"`
	Relation  RelationType `json:"relation"`
	Weight    float64      `json:"weight"`
	CreatedAt time.Time    `json:"created_at"`
}

type NodeSummary struct {
	ID                string   `json:"id"`
	Type              NodeType `json:"type"`
	Title             string   `json:"title"`
	Summary           string   `json:"summary"`
	RetrievalTriggers []string `json:"retrieval_triggers,omitempty"`
}
