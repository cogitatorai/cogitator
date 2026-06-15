package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/cogitatorai/cogitator/server/internal/config"
	"github.com/cogitatorai/cogitator/server/internal/database"
	"github.com/cogitatorai/cogitator/server/internal/memory"
	"github.com/cogitatorai/cogitator/server/internal/provider"
	"github.com/cogitatorai/cogitator/server/internal/session"
	"github.com/cogitatorai/cogitator/server/internal/worker"
)

// RunConfig controls a single evaluation run.
type RunConfig struct {
	Provider     provider.Provider
	ProviderName string
	Model        string
	// DataDir is the root directory containing per-stage subdirectories.
	// Each stage directory must contain a "cases.json" file.
	DataDir string
	// CacheDir is optional; if empty, no caching is performed.
	CacheDir string
	// Stages lists which stages to run: "enrichment", "retrieval", "reflection".
	// If empty, all stages with a cases.json present are run.
	Stages []string

	// RetrievalCasesFile overrides the retrieval cases file name under
	// <DataDir>/retrieval/. Empty means "cases.json".
	RetrievalCasesFile string

	// Retrieval embedding. Embedder is used for both seeding fixtures and the
	// query. EmbeddingModel labels stored vectors. When Embedder is nil the
	// retrieval stage falls back to the legacy no-embedder (LLM) path.
	Embedder       provider.Embedder
	EmbeddingModel string
}

// Run executes the evaluation and returns a Report.
func Run(ctx context.Context, cfg RunConfig) (*Report, error) {
	var cache *Cache
	if cfg.CacheDir != "" {
		cache = NewCache(cfg.CacheDir)
	}

	stages := cfg.Stages
	if len(stages) == 0 {
		stages = []string{"enrichment", "retrieval", "reflection"}
	}

	report := &Report{
		Provider: cfg.ProviderName,
		Model:    cfg.Model,
	}

	for _, stage := range stages {
		casesFile := "cases.json"
		if stage == "retrieval" && cfg.RetrievalCasesFile != "" {
			casesFile = cfg.RetrievalCasesFile
		}
		casesPath := filepath.Join(cfg.DataDir, stage, casesFile)
		if _, err := os.Stat(casesPath); os.IsNotExist(err) {
			continue
		}

		var stageResult StageResult
		var err error

		switch stage {
		case "enrichment":
			stageResult, err = runEnrichment(ctx, cfg, cache, casesPath)
		case "retrieval":
			stageResult, err = runRetrieval(ctx, cfg, casesPath)
		case "reflection":
			stageResult, err = runReflection(cfg, casesPath)
		default:
			return nil, fmt.Errorf("unknown stage: %s", stage)
		}
		if err != nil {
			return nil, fmt.Errorf("stage %s: %w", stage, err)
		}

		report.Stages = append(report.Stages, stageResult)
	}

	// Compute overall score: average of stage metric averages.
	if len(report.Stages) > 0 {
		var total float64
		for _, s := range report.Stages {
			var stageAvg float64
			for _, v := range s.Metrics {
				stageAvg += v
			}
			if len(s.Metrics) > 0 {
				stageAvg /= float64(len(s.Metrics))
			}
			total += stageAvg
		}
		report.Total = total / float64(len(report.Stages))
	}

	return report, nil
}

// runEnrichment runs all enrichment cases and returns their aggregated stage result.
func runEnrichment(ctx context.Context, cfg RunConfig, cache *Cache, casesPath string) (StageResult, error) {
	data, err := os.ReadFile(casesPath)
	if err != nil {
		return StageResult{}, err
	}
	var cases []EnrichmentCase
	if err := json.Unmarshal(data, &cases); err != nil {
		return StageResult{}, fmt.Errorf("parse enrichment cases: %w", err)
	}

	stage := StageResult{
		Stage:   "enrichment",
		Cases:   len(cases),
		Metrics: make(map[string]float64),
	}

	metricSums := make(map[string]float64)
	metricCounts := make(map[string]int)

	for _, c := range cases {
		cr := runEnrichmentCase(ctx, cfg, cache, c)
		stage.Results = append(stage.Results, cr)
		if cr.Error == "" {
			for k, v := range cr.Scores {
				metricSums[k] += v
				metricCounts[k]++
			}
		}
		if cr.Cached {
			stage.Cached++
		}
	}

	for k, sum := range metricSums {
		stage.Metrics[k] = sum / float64(metricCounts[k])
	}
	return stage, nil
}

func runEnrichmentCase(ctx context.Context, cfg RunConfig, cache *Cache, c EnrichmentCase) CaseResult {
	cr := CaseResult{ID: c.ID, Stage: "enrichment", Scores: make(map[string]float64)}

	node := memory.Node{
		ID:    c.ID,
		Type:  memory.NodeFact, // placeholder; enrichment will reclassify
		Title: c.Input.Title,
	}
	prompt := worker.BuildEnrichmentPrompt(node, c.Input.Content, "")

	var responseText string
	var hitCache bool
	if cache != nil {
		key := CacheKey(prompt, cfg.ProviderName, cfg.Model)
		if cached, ok := cache.Get(key); ok {
			responseText = cached
			hitCache = true
		}
	}

	if responseText == "" {
		resp, err := cfg.Provider.Chat(ctx, []provider.Message{
			{Role: "user", Content: prompt},
		}, nil, cfg.Model, nil)
		if err != nil {
			cr.Error = err.Error()
			return cr
		}
		responseText = resp.Content
		if cache != nil {
			key := CacheKey(prompt, cfg.ProviderName, cfg.Model)
			cache.Put(key, responseText)
		}
	}

	cleaned := cleanLLMJSON(responseText)

	var result worker.EnrichResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		cr.Error = fmt.Sprintf("parse enrich response: %v", err)
		return cr
	}

	cr.Scores = ScoreEnrichment(c, result.NodeType, result.Tags, result.Summary)
	cr.Cached = hitCache
	cr.Pass = cr.Scores["type_accuracy"] == 1.0 &&
		cr.Scores["summary_quality"] == 1.0 &&
		cr.Scores["tag_overlap"] >= c.Expected.TagMinOverlap
	return cr
}

// runRetrieval seeds an in-memory SQLite database with fixtures, runs the
// retriever against each case query, and scores the returned node IDs.
func runRetrieval(ctx context.Context, cfg RunConfig, casesPath string) (StageResult, error) {
	data, err := os.ReadFile(casesPath)
	if err != nil {
		return StageResult{}, err
	}
	var cases []RetrievalCase
	if err := json.Unmarshal(data, &cases); err != nil {
		return StageResult{}, fmt.Errorf("parse retrieval cases: %w", err)
	}

	// Load fixtures if present alongside the cases file. The deterministic
	// mechanics suite uses its own hand-built graph in mechanics_fixtures.json.
	base := filepath.Base(casesPath)
	fixturesName := "fixtures.json"
	if base == "mechanics.json" {
		fixturesName = "mechanics_fixtures.json"
	}
	fixturesPath := filepath.Join(filepath.Dir(casesPath), fixturesName)
	var fixtures []RetrievalFixture
	if fdata, ferr := os.ReadFile(fixturesPath); ferr == nil {
		if jerr := json.Unmarshal(fdata, &fixtures); jerr != nil {
			return StageResult{}, fmt.Errorf("parse retrieval fixtures: %w", jerr)
		}
	}

	// Open a temp-file SQLite database and seed it with fixtures. A file path
	// (rather than ":memory:") is required because the database uses separate
	// writer and reader connections; an in-memory database is isolated per
	// connection, so the reader would not see tables seeded by the writer (e.g.
	// node_embeddings on the vector path).
	tmpDir, err := os.MkdirTemp("", "eval-retrieval-*")
	if err != nil {
		return StageResult{}, fmt.Errorf("create eval tempdir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := database.Open(filepath.Join(tmpDir, "eval.db"), database.Options{})
	if err != nil {
		return StageResult{}, fmt.Errorf("open eval db: %w", err)
	}
	defer db.Close()

	store := memory.NewStore(db)
	cm := memory.NewContentManager(tmpDir)

	// fixtureIDMap maps fixture ID (from JSON) to the actual node ID assigned
	// by CreateNode. This remapping is necessary because CreateNode generates
	// its own ULID and ignores any pre-set ID.
	fixtureIDMap := make(map[string]string, len(fixtures))

	// When an embedder is configured, seed embeddings via the production
	// NodeEmbedder so retrieval exercises the real vector path.
	var nodeEmbedder *memory.NodeEmbedder
	if cfg.Embedder != nil {
		nodeEmbedder = memory.NewNodeEmbedder(store, cm, cfg.Embedder, cfg.EmbeddingModel, slog.Default())
	}

	for _, f := range fixtures {
		n := &memory.Node{
			Type:              memory.NodeType(f.Type),
			Title:             f.Title,
			Summary:           f.Summary,
			Tags:              f.Tags,
			RetrievalTriggers: f.RetrievalTriggers,
			Pinned:            f.Pinned,
			EnrichmentStatus:  memory.EnrichmentComplete,
			Confidence:        1.0,
		}
		if f.UserID != "" {
			uid := f.UserID
			n.UserID = &uid
		}
		actualID, cerr := store.CreateNode(n)
		if cerr != nil {
			return StageResult{}, fmt.Errorf("seed fixture %q: %w", f.ID, cerr)
		}
		n.ID = actualID
		// Write content if provided, and record the content path on the node so
		// the embedder includes the body text.
		if f.Content != "" {
			path, werr := cm.Write(actualID, f.Content)
			if werr != nil {
				return StageResult{}, fmt.Errorf("write fixture content %q: %w", f.ID, werr)
			}
			n.ContentPath = path
		}
		if nodeEmbedder != nil {
			if eerr := nodeEmbedder.EmbedNode(ctx, n); eerr != nil {
				return StageResult{}, fmt.Errorf("embed fixture %q: %w", f.ID, eerr)
			}
		}
		fixtureIDMap[f.ID] = actualID
	}

	// Seed edges after all fixture IDs are mapped so cross-references resolve.
	for _, f := range fixtures {
		src, ok := fixtureIDMap[f.ID]
		if !ok {
			continue
		}
		for _, e := range f.Edges {
			tgt, ok := fixtureIDMap[e.Target]
			if !ok {
				return StageResult{}, fmt.Errorf("fixture %q edge target %q not found", f.ID, e.Target)
			}
			if cerr := store.CreateEdge(&memory.Edge{
				SourceID: src,
				TargetID: tgt,
				Relation: memory.RelRelatedTo,
				Weight:   e.Weight,
			}); cerr != nil {
				return StageResult{}, fmt.Errorf("seed edge %q->%q: %w", f.ID, e.Target, cerr)
			}
		}
	}

	mem := config.Default().Memory
	retriever := memory.NewRetriever(memory.RetrieverConfig{
		Store:          store,
		Content:        cm,
		Provider:       cfg.Provider,
		Model:          cfg.Model,
		Embedder:       cfg.Embedder,
		EmbeddingModel: cfg.EmbeddingModel,
		TopK:           mem.RetrievalTopK,
		TokenBudget:    mem.RetrievalTokenBudget,
		MinSimilarity:  mem.RetrievalMinSimilarity,
		TypeBoost:      mem.RetrievalTypeBoost,
		ContextWindow:  mem.ContextWindow,
	})

	stage := StageResult{
		Stage:   "retrieval",
		Cases:   len(cases),
		Metrics: make(map[string]float64),
	}

	metricSums := make(map[string]float64)
	metricCounts := make(map[string]int)

	for _, c := range cases {
		// Remap expected IDs from fixture IDs to actual store IDs.
		remapped := remapIDs(c, fixtureIDMap)

		cr := runRetrievalCase(ctx, retriever, remapped)
		stage.Results = append(stage.Results, cr)
		if cr.Error == "" {
			for k, v := range cr.Scores {
				metricSums[k] += v
				metricCounts[k]++
			}
		}
	}

	for k, sum := range metricSums {
		stage.Metrics[k] = sum / float64(metricCounts[k])
	}
	return stage, nil
}

// remapIDs translates fixture IDs in a RetrievalCase to actual store IDs.
func remapIDs(c RetrievalCase, idMap map[string]string) RetrievalCase {
	out := c
	out.ExpectedIDs = make([]string, 0, len(c.ExpectedIDs))
	for _, id := range c.ExpectedIDs {
		if actual, ok := idMap[id]; ok {
			out.ExpectedIDs = append(out.ExpectedIDs, actual)
		} else {
			out.ExpectedIDs = append(out.ExpectedIDs, id)
		}
	}
	out.ExpectedNotIDs = make([]string, 0, len(c.ExpectedNotIDs))
	for _, id := range c.ExpectedNotIDs {
		if actual, ok := idMap[id]; ok {
			out.ExpectedNotIDs = append(out.ExpectedNotIDs, actual)
		} else {
			out.ExpectedNotIDs = append(out.ExpectedNotIDs, id)
		}
	}
	return out
}

// TraceCandidateView is a minimal view of a trace candidate used to build
// drop diagnostics for expected-but-missed nodes.
type TraceCandidateView struct {
	DropReason string
	Similarity float64
}

func runRetrievalCase(ctx context.Context, retriever *memory.Retriever, c RetrievalCase) CaseResult {
	cr := CaseResult{ID: c.ID, Stage: "retrieval", Scores: make(map[string]float64)}

	// Request a trace for this turn so we can explain why expected nodes were
	// dropped on the vector path.
	tctx, holder := memory.WithTrace(ctx)

	result, err := retriever.Retrieve(tctx, c.UserID, c.Query, c.History)
	if err != nil {
		cr.Error = err.Error()
		return cr
	}

	var returnedIDs []string
	injected := make(map[string]bool, len(result.Nodes))
	for _, n := range result.Nodes {
		returnedIDs = append(returnedIDs, n.Node.ID)
		injected[n.Node.ID] = true
	}

	cr.Scores = ScoreRetrieval(c, returnedIDs)
	cr.Pass = cr.Scores["precision"] >= c.MinPrecision &&
		cr.Scores["recall"] >= c.MinRecall &&
		cr.Scores["exclusion"] == 1.0

	// Build a candidate lookup from the trace, then attach a diagnostic for each
	// expected node that was not injected.
	candidates := make(map[string]TraceCandidateView)
	if tr := holder.Get(); tr != nil {
		for _, tc := range tr.Candidates {
			candidates[tc.NodeID] = TraceCandidateView{DropReason: tc.DropReason, Similarity: tc.Similarity}
		}
	}
	for _, id := range c.ExpectedIDs {
		if injected[id] {
			continue
		}
		d := DropDiagnostic{NodeID: id, DropReason: "not_a_candidate"}
		if cv, ok := candidates[id]; ok {
			d.DropReason = cv.DropReason
			d.Similarity = cv.Similarity
		}
		cr.Diagnostics = append(cr.Diagnostics, d)
	}

	return cr
}

// runReflection runs all reflection cases using pattern-based DetectSignals.
// No LLM call is needed for English pattern matching.
func runReflection(cfg RunConfig, casesPath string) (StageResult, error) {
	data, err := os.ReadFile(casesPath)
	if err != nil {
		return StageResult{}, err
	}
	var cases []ReflectionCase
	if err := json.Unmarshal(data, &cases); err != nil {
		return StageResult{}, fmt.Errorf("parse reflection cases: %w", err)
	}

	stage := StageResult{
		Stage:   "reflection",
		Cases:   len(cases),
		Metrics: make(map[string]float64),
	}

	metricSums := make(map[string]float64)
	metricCounts := make(map[string]int)

	for _, c := range cases {
		cr := runReflectionCase(c)
		stage.Results = append(stage.Results, cr)
		if cr.Error == "" {
			for k, v := range cr.Scores {
				metricSums[k] += v
				metricCounts[k]++
			}
		}
	}

	for k, sum := range metricSums {
		stage.Metrics[k] = sum / float64(metricCounts[k])
	}
	return stage, nil
}

func runReflectionCase(c ReflectionCase) CaseResult {
	cr := CaseResult{ID: c.ID, Stage: "reflection", Scores: make(map[string]float64)}

	// Convert provider.Message to session.Message for DetectSignals.
	msgs := make([]session.Message, 0, len(c.Messages))
	for _, m := range c.Messages {
		content := ""
		switch v := m.Content.(type) {
		case string:
			content = v
		}
		msgs = append(msgs, session.Message{
			Role:    m.Role,
			Content: content,
		})
	}

	signals := worker.DetectSignals(msgs)

	// Use the first detected signal, if any.
	var signalType string
	var confidence float64
	if len(signals) > 0 {
		signalType = signals[0].Type
		confidence = signals[0].Confidence
	}

	cr.Scores = ScoreReflection(c, signalType, confidence)
	cr.Pass = cr.Scores["signal_accuracy"] == 1.0
	return cr
}

// cleanLLMJSON strips common LLM artifacts from JSON responses:
// markdown code fences, single-line comments, and trailing commas.
func cleanLLMJSON(s string) string {
	// Strip code fences.
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)

	// Strip single-line comments (// ...).
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		// Remove inline comments after values (e.g., `"key": "val" // comment`).
		// Only strip if // appears outside of a quoted string.
		if idx := findUnquotedComment(line); idx >= 0 {
			line = line[:idx]
		}
		lines = append(lines, line)
	}
	s = strings.Join(lines, "\n")

	// Strip trailing commas before } or ].
	s = strings.ReplaceAll(s, ",\n}", "\n}")
	s = strings.ReplaceAll(s, ",\n]", "\n]")
	// Handle single-line trailing commas too.
	s = strings.ReplaceAll(s, ",}", "}")
	s = strings.ReplaceAll(s, ",]", "]")

	return s
}

// findUnquotedComment returns the index of // that appears outside quoted strings,
// or -1 if none found.
func findUnquotedComment(s string) int {
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		if escaped {
			escaped = false
			continue
		}
		switch s[i] {
		case '\\':
			if inString {
				escaped = true
			}
		case '"':
			inString = !inString
		case '/':
			if !inString && i+1 < len(s) && s[i+1] == '/' {
				return i
			}
		}
	}
	return -1
}
