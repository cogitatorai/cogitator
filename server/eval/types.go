package eval

import "github.com/cogitatorai/cogitator/server/internal/provider"

// EnrichmentCase is a single enrichment evaluation test case.
type EnrichmentCase struct {
	ID       string             `json:"id"`
	Input    EnrichmentInput    `json:"input"`
	Expected EnrichmentExpected `json:"expected"`
}

type EnrichmentInput struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

type EnrichmentExpected struct {
	NodeType              string   `json:"node_type"`
	Tags                  []string `json:"tags"`
	TagMinOverlap         float64  `json:"tag_min_overlap"`
	SummaryMustContain    []string `json:"summary_must_contain"`
	SummaryMustNotContain []string `json:"summary_must_not_contain"`
}

// RetrievalCase is a single retrieval evaluation test case.
type RetrievalCase struct {
	ID             string   `json:"id"`
	Query          string   `json:"query"`
	ExpectedIDs    []string `json:"expected_node_ids"`
	ExpectedNotIDs []string `json:"expected_not_ids"`
	MinPrecision   float64  `json:"min_precision"`
	MinRecall      float64  `json:"min_recall"`
}

// RetrievalFixture is a pre-seeded node for retrieval tests.
type RetrievalFixture struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Title     string    `json:"title"`
	Summary   string    `json:"summary"`
	Tags      []string  `json:"tags"`
	Content   string    `json:"content"`
	Embedding []float32 `json:"embedding"`
}

// ReflectionCase is a single reflection evaluation test case.
type ReflectionCase struct {
	ID             string             `json:"id"`
	Messages       []provider.Message `json:"messages"`
	ExpectedSignal string             `json:"expected_signal"`
	MinConfidence  float64            `json:"min_confidence"`
}

// CaseResult holds the score for a single test case.
type CaseResult struct {
	ID     string             `json:"id"`
	Stage  string             `json:"stage"`
	Scores map[string]float64 `json:"scores"`
	Pass   bool               `json:"pass"`
	Cached bool               `json:"cached"`
	Error  string             `json:"error,omitempty"`
}

// StageResult aggregates results for one evaluation stage.
type StageResult struct {
	Stage   string             `json:"stage"`
	Cases   int                `json:"cases"`
	Cached  int                `json:"cached"`
	Metrics map[string]float64 `json:"metrics"`
	Results []CaseResult       `json:"results"`
}

// Report is the full evaluation output.
type Report struct {
	Provider string        `json:"provider"`
	Model    string        `json:"model"`
	Stages   []StageResult `json:"stages"`
	Total    float64       `json:"total_score"`
}
