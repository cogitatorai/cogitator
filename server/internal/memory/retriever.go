package memory

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/cogitatorai/cogitator/server/internal/provider"
)

// Retriever performs memory retrieval using vector similarity when an embedder
// is configured, falling back to LLM classification when it is not.
type Retriever struct {
	mu               sync.RWMutex
	store            *Store
	content          *ContentManager
	provider         provider.Provider
	model            string
	standardProvider provider.Provider
	standardModel    string
	topK             int
	edgeMinWeight    float64
	logger           *slog.Logger

	// Vector retrieval fields.
	embedder       provider.Embedder
	embeddingModel string
	cacheDirty    bool
	contextWindow int
	tokenBudget    int
	minSimilarity  float64
	typeBoost      float64
	types          []NodeType

	// Enriched cache: userID -> nodeID -> EmbeddingMeta
	metaCache   map[string]map[string]EmbeddingMeta
	cachedTypes []NodeType // types used to populate the cache
}

// RetrieverConfig holds configuration for constructing a Retriever.
type RetrieverConfig struct {
	Store            *Store
	Content          *ContentManager
	Provider         provider.Provider
	Model            string
	StandardProvider provider.Provider
	StandardModel    string
	TopK             int     // defaults to 5
	EdgeMinWeight    float64 // minimum edge weight to follow, defaults to 0.5
	Logger           *slog.Logger

	// Vector retrieval configuration.
	Embedder       provider.Embedder
	EmbeddingModel string
	ContextWindow int // defaults to 5
	TokenBudget    int        // max estimated tokens of retrieved context, defaults to 2000
	MinSimilarity  float64    // cosine similarity floor, defaults to 0.3
	TypeBoost      float64    // score multiplier for preference/fact, defaults to 1.1
	Types          []NodeType // retrievable types, defaults to fact/preference/pattern/skill
}

// NewRetriever constructs a Retriever from the given config, applying defaults
// for any zero values.
func NewRetriever(cfg RetrieverConfig) *Retriever {
	if cfg.TopK <= 0 {
		cfg.TopK = 5
	}
	if cfg.EdgeMinWeight <= 0 {
		cfg.EdgeMinWeight = 0.5
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.ContextWindow <= 0 {
		cfg.ContextWindow = 5
	}
	if cfg.TokenBudget <= 0 {
		cfg.TokenBudget = 2000
	}
	if cfg.MinSimilarity <= 0 {
		cfg.MinSimilarity = 0.3
	}
	if cfg.TypeBoost <= 0 {
		cfg.TypeBoost = 1.1
	}
	if len(cfg.Types) == 0 {
		cfg.Types = []NodeType{NodeFact, NodePreference, NodePattern, NodeSkill}
	}
	return &Retriever{
		store:            cfg.Store,
		content:          cfg.Content,
		provider:         cfg.Provider,
		model:            cfg.Model,
		standardProvider: cfg.StandardProvider,
		standardModel:    cfg.StandardModel,
		topK:             cfg.TopK,
		edgeMinWeight:    cfg.EdgeMinWeight,
		logger:           cfg.Logger,
		embedder:         cfg.Embedder,
		embeddingModel:   cfg.EmbeddingModel,
		cacheDirty:    true,
		contextWindow: cfg.ContextWindow,
		tokenBudget:      cfg.TokenBudget,
		minSimilarity:    cfg.MinSimilarity,
		typeBoost:        cfg.TypeBoost,
		types:            cfg.Types,
	}
}

// SetProvider updates the LLM provider and model used for classification.
// This allows the retriever to start working after the user configures a provider.
func (r *Retriever) SetProvider(p provider.Provider, model string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.provider = p
	if model != "" {
		r.model = model
	}
}

// SetStandardProvider updates the standard-tier LLM provider used for
// association expansion in two-stage retrieval.
func (r *Retriever) SetStandardProvider(p provider.Provider, model string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.standardProvider = p
	if model != "" {
		r.standardModel = model
	}
}

// SetEmbedder updates the embedder and embedding model at runtime.
func (r *Retriever) SetEmbedder(e provider.Embedder, model string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.embedder = e
	if model != "" {
		r.embeddingModel = model
	}
}

// InvalidateCache marks the embedding cache as dirty so it is refreshed on the
// next Retrieve call. Call this whenever nodes are created or updated.
func (r *Retriever) InvalidateCache() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metaCache = nil
	r.cachedTypes = nil
	r.cacheDirty = true
}

// NameResolver maps a user ID to a display name. Returns empty string if unknown.
type NameResolver func(userID string) string

// RetrievedContext is the assembled memory context for a message.
type RetrievedContext struct {
	// Pinned holds always-present pinned memories, independent of top-K.
	Pinned []RetrievedNode
	// Nodes holds the top-K fully loaded nodes with their content.
	Nodes []RetrievedNode
	// Connected holds 1-hop neighbors reachable via high-weight edges,
	// represented as summaries only.
	Connected []NodeSummary
}

// RetrievedNode pairs a full Node with the text of its content file.
type RetrievedNode struct {
	Node    Node
	Content string
}

// String formats the retrieved context for injection into a system prompt.
// Returns an empty string when there are no pinned nodes and no retrieved nodes.
func (rc RetrievedContext) String() string {
	return rc.Format(nil, "")
}

// Format formats the retrieved context for injection into a system prompt.
// When resolve is non-nil, memories are annotated with subject ("about {name}")
// and owner ("shared by {name}") information. currentUserID identifies the
// requesting user so that memories without an explicit subject can be labeled
// as belonging to them.
func (rc RetrievedContext) Format(resolve NameResolver, currentUserID string) string {
	if len(rc.Pinned) == 0 && len(rc.Nodes) == 0 {
		return ""
	}
	var b strings.Builder

	writeNode := func(n RetrievedNode) {
		typeSuffix := string(n.Node.Type)
		if resolve != nil && n.Node.SubjectID != nil {
			if name := resolve(*n.Node.SubjectID); name != "" {
				typeSuffix += ", about " + name
			}
		}
		if resolve != nil && n.Node.UserID != nil {
			if name := resolve(*n.Node.UserID); name != "" {
				typeSuffix += ", shared by " + name
			}
		}
		header := "#### " + n.Node.Title + " (" + typeSuffix + ")"
		b.WriteString(header + "\n")
		if n.Content != "" {
			b.WriteString(n.Content + "\n\n")
		} else if n.Node.Summary != "" {
			b.WriteString(n.Node.Summary + "\n\n")
		}
	}

	b.WriteString("## Memory Instructions\nThe memories below are things you know about the user. You MUST actively use them to personalize your responses. NEVER ask the user for information that is already contained in these memories. When a memory is relevant, weave it in naturally (e.g., \"since your daughter is 9...\" or \"knowing you enjoy hiking...\").\n\n")

	if len(rc.Pinned) > 0 {
		b.WriteString("### Pinned Memories\n")
		for _, n := range rc.Pinned {
			writeNode(n)
		}
	}

	if len(rc.Nodes) > 0 {
		b.WriteString("### Retrieved Memories\n")
		for _, n := range rc.Nodes {
			writeNode(n)
		}
	}

	if len(rc.Connected) > 0 {
		b.WriteString("### Related Knowledge\n")
		for _, s := range rc.Connected {
			b.WriteString("- " + s.Title)
			if s.Summary != "" {
				b.WriteString(": " + s.Summary)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// Retrieve finds relevant memory nodes for the given message. When an embedder
// is configured it uses vector similarity; otherwise it falls back to LLM
// classification. The history slice (last N conversation messages) enriches
// the query context for the vector path.
func (r *Retriever) Retrieve(ctx context.Context, userID, message string, history []provider.Message) (*RetrievedContext, error) {
	r.mu.RLock()
	emb := r.embedder
	r.mu.RUnlock()

	if emb != nil {
		return r.retrieveVector(ctx, userID, message, history)
	}
	return r.retrieveLLM(ctx, userID, message)
}

// typesMatch returns true when a and b contain the same types (order-independent).
func typesMatch(a, b []NodeType) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[NodeType]struct{}, len(a))
	for _, t := range a {
		set[t] = struct{}{}
	}
	for _, t := range b {
		if _, ok := set[t]; !ok {
			return false
		}
	}
	return true
}

// retrieveVector performs embedding-based retrieval with type filtering,
// similarity threshold, type boost scoring, and token-budget-based fill.
func (r *Retriever) retrieveVector(ctx context.Context, userID, message string, history []provider.Message) (*RetrievedContext, error) {
	r.mu.RLock()
	emb := r.embedder
	embModel := r.embeddingModel
	ctxWindow := r.contextWindow
	budget := r.tokenBudget
	minSim := r.minSimilarity
	tBoost := r.typeBoost
	types := r.types
	r.mu.RUnlock()

	// Build retrieval text from recent history + current message.
	queryText := buildRetrievalText(message, history, ctxWindow)

	// Embed the query.
	vecs, err := emb.Embed(ctx, []string{queryText}, embModel)
	if err != nil {
		return nil, err
	}
	queryVec := vecs[0]

	// Refresh metadata cache for this user if dirty, missing, or types changed.
	cacheKey := userID
	if cacheKey == "" {
		cacheKey = "_all"
	}
	r.mu.Lock()
	if r.metaCache == nil {
		r.metaCache = make(map[string]map[string]EmbeddingMeta)
	}
	if r.cacheDirty || r.metaCache[cacheKey] == nil || !typesMatch(r.cachedTypes, types) {
		userCache, cErr := r.store.GetEmbeddingsWithMeta(userID, types)
		if cErr != nil {
			r.mu.Unlock()
			return nil, cErr
		}
		r.metaCache[cacheKey] = userCache
		r.cachedTypes = types
		r.cacheDirty = false
	}
	// Snapshot the cache under lock so we can release quickly.
	cache := make(map[string]EmbeddingMeta, len(r.metaCache[cacheKey]))
	for k, v := range r.metaCache[cacheKey] {
		cache[k] = v
	}
	r.mu.Unlock()

	// Load pinned nodes first (always included, don't count against budget).
	pinnedNodes, err := r.store.GetPinnedNodes(userID)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool, len(pinnedNodes))
	result := &RetrievedContext{}

	for _, pn := range pinnedNodes {
		seen[pn.ID] = true
		_ = r.store.TouchAccess(pn.ID)
		content := r.loadContent(pn.ContentPath)
		result.Pinned = append(result.Pinned, RetrievedNode{Node: pn, Content: content})
	}

	// Score all cached embeddings with similarity threshold and type boost.
	type scored struct {
		id            string
		score         float64
		contentLength int
	}
	var candidates []scored

	for id, meta := range cache {
		if seen[id] {
			continue
		}
		sim := CosineSimilarity(queryVec, meta.Embedding)

		// Drop candidates below similarity threshold.
		if sim < minSim {
			continue
		}

		score := sim * meta.Confidence

		// Apply type boost for preference and fact nodes.
		if meta.Type == NodePreference || meta.Type == NodeFact {
			score *= tBoost
		}

		candidates = append(candidates, scored{
			id:            id,
			score:         score,
			contentLength: meta.ContentLength,
		})
	}

	// Sort descending by score.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	// Fill results using token budget.
	tokensUsed := 0
	var selected []scored
	for _, c := range candidates {
		est := estimateTokensFromLength(c.contentLength)
		if tokensUsed+est > budget && len(selected) > 0 {
			break
		}
		tokensUsed += est
		selected = append(selected, c)
	}

	// Load full node + content for selected candidates only.
	for _, c := range selected {
		node, err := r.store.GetNode(c.id)
		if err != nil {
			continue
		}
		seen[c.id] = true
		_ = r.store.TouchAccess(c.id)
		_ = r.store.AdjustConfidence(c.id, 0.02, 0.95)
		content := r.loadContent(node.ContentPath)
		result.Nodes = append(result.Nodes, RetrievedNode{Node: *node, Content: content})
	}

	// Follow 1-hop high-weight edges from every loaded node (pinned + retrieved).
	allLoaded := make([]string, 0, len(result.Pinned)+len(result.Nodes))
	for _, rn := range result.Pinned {
		allLoaded = append(allLoaded, rn.Node.ID)
	}
	for _, rn := range result.Nodes {
		allLoaded = append(allLoaded, rn.Node.ID)
	}

	r.mu.RLock()
	minWeight := r.edgeMinWeight
	r.mu.RUnlock()

	for _, id := range allLoaded {
		edges, err := r.store.GetEdgesFrom(id, userID)
		if err != nil {
			continue
		}
		for _, edge := range edges {
			if edge.Weight < minWeight || seen[edge.TargetID] {
				continue
			}
			seen[edge.TargetID] = true
			target, err := r.store.GetNode(edge.TargetID)
			if err != nil {
				continue
			}
			result.Connected = append(result.Connected, NodeSummary{
				ID:      target.ID,
				Type:    target.Type,
				Title:   target.Title,
				Summary: target.Summary,
			})
		}
	}

	r.logger.Info("retrieval: vector path",
		"query_len", len(queryText),
		"pinned", len(result.Pinned),
		"nodes", len(result.Nodes),
		"connected", len(result.Connected),
		"tokens_used", tokensUsed,
	)

	return result, nil
}

// retrieveLLM is the original LLM-classification-based retrieval, preserved as
// the fallback when no embedder is configured.
func (r *Retriever) retrieveLLM(ctx context.Context, userID, message string) (*RetrievedContext, error) {
	r.mu.RLock()
	p := r.provider
	model := r.model
	r.mu.RUnlock()

	if p == nil {
		return &RetrievedContext{}, nil
	}
	summaries, err := r.store.GetNodeSummaries(userID)
	if err != nil {
		return nil, err
	}
	if len(summaries) == 0 {
		return &RetrievedContext{}, nil
	}

	associations := r.expandAssociations(ctx, message)
	prompt := buildClassificationPrompt(message, summaries, associations)

	messages := []provider.Message{
		{Role: "system", Content: "You are a memory retrieval classifier. Respond ONLY with a JSON array of node IDs, most relevant first."},
		{Role: "user", Content: prompt},
	}

	resp, err := p.Chat(ctx, messages, nil, model, nil)
	if err != nil {
		return nil, err
	}

	nodeIDs, err := parseNodeIDs(resp.Content)
	if err != nil {
		r.logger.Warn("failed to parse retrieval response", "error", err, "response", resp.Content)
		return &RetrievedContext{}, nil
	}

	r.logger.Info("retrieval: LLM path nodes selected",
		"node_ids", nodeIDs,
		"count", len(nodeIDs),
	)

	r.mu.RLock()
	topK := r.topK
	minWeight := r.edgeMinWeight
	r.mu.RUnlock()

	if len(nodeIDs) > topK {
		nodeIDs = nodeIDs[:topK]
	}

	result := &RetrievedContext{}
	seen := make(map[string]bool)

	for _, id := range nodeIDs {
		node, err := r.store.GetNode(id)
		if err != nil {
			continue
		}
		seen[id] = true
		// Best-effort: access tracking failure must not abort retrieval.
		_ = r.store.TouchAccess(id)
		_ = r.store.AdjustConfidence(id, 0.02, 0.95)

		content := r.loadContent(node.ContentPath)
		result.Nodes = append(result.Nodes, RetrievedNode{
			Node:    *node,
			Content: content,
		})
	}

	// Follow 1-hop high-weight edges from every loaded node.
	for _, id := range nodeIDs {
		edges, err := r.store.GetEdgesFrom(id, userID)
		if err != nil {
			continue
		}
		for _, edge := range edges {
			if edge.Weight < minWeight || seen[edge.TargetID] {
				continue
			}
			seen[edge.TargetID] = true
			target, err := r.store.GetNode(edge.TargetID)
			if err != nil {
				continue
			}
			result.Connected = append(result.Connected, NodeSummary{
				ID:      target.ID,
				Type:    target.Type,
				Title:   target.Title,
				Summary: target.Summary,
			})
		}
	}

	return result, nil
}

// loadContent reads content from the ContentManager. Returns an empty string on
// any error or if no content path is set.
func (r *Retriever) loadContent(contentPath string) string {
	if contentPath == "" || r.content == nil {
		return ""
	}
	c, err := r.content.Read(contentPath)
	if err != nil {
		return ""
	}
	return c
}

// buildRetrievalText constructs the query text from recent history and the
// current user message. At most contextWindow messages from history are used.
func buildRetrievalText(message string, history []provider.Message, contextWindow int) string {
	var b strings.Builder
	start := 0
	if len(history) > contextWindow {
		start = len(history) - contextWindow
	}
	for _, m := range history[start:] {
		b.WriteString(m.Role + ": " + m.ContentText() + "\n")
	}
	b.WriteString("User: " + message)
	return b.String()
}

// expandAssociations uses the standard-tier LLM to brainstorm themes and
// associations from the user message. Returns nil if no standard provider
// is configured (graceful fallback to single-stage retrieval).
func (r *Retriever) expandAssociations(ctx context.Context, message string) []string {
	r.mu.RLock()
	p := r.standardProvider
	model := r.standardModel
	r.mu.RUnlock()

	if p == nil {
		return nil
	}

	messages := []provider.Message{
		{Role: "system", Content: `You are an association engine for a personal knowledge graph.
The graph stores these types of user knowledge: facts, preferences, patterns, skills, episodes, and task_knowledge.

Given a user message, brainstorm themes and associations that might connect to the user's stored knowledge. Think broadly: cultural references, geographic associations, related hobbies, and indirect connections.

Respond ONLY with a JSON array of short theme strings.`},
		{Role: "user", Content: message},
	}

	resp, err := p.Chat(ctx, messages, nil, model, nil)
	if err != nil {
		r.logger.Warn("association expansion failed", "error", err)
		return nil
	}

	var themes []string
	cleaned := strings.TrimSpace(resp.Content)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	if err := json.Unmarshal([]byte(cleaned), &themes); err != nil {
		r.logger.Warn("failed to parse associations", "error", err, "response", resp.Content)
		return nil
	}

	r.logger.Info("retrieval: associations expanded",
		"message_prefix", truncate(message, 80),
		"associations", themes,
	)
	return themes
}

// buildClassificationPrompt constructs the user-facing prompt sent to the
// classifier LLM.
func buildClassificationPrompt(message string, summaries []NodeSummary, associations []string) string {
	var b strings.Builder
	b.WriteString("Given the user message below, select the most relevant memory nodes.\n")
	if len(associations) > 0 {
		b.WriteString("Consider both direct matches and thematic connections between the expanded associations and node content.\n\n")
		b.WriteString("User message: " + message + "\n\n")
		assocJSON, _ := json.Marshal(associations)
		b.WriteString("Expanded associations: " + string(assocJSON) + "\n\n")
	} else {
		b.WriteString("\nUser message: " + message + "\n\n")
	}
	b.WriteString("Available nodes:\n")
	for _, s := range summaries {
		b.WriteString("- [" + s.ID + "] " + string(s.Type) + ": " + s.Title)
		if s.Summary != "" {
			b.WriteString(" | " + s.Summary)
		}
		if len(s.RetrievalTriggers) > 0 {
			b.WriteString(" (triggers: " + strings.Join(s.RetrievalTriggers, ", ") + ")")
		}
		b.WriteString("\n")
	}
	b.WriteString("\nReturn a JSON array of node IDs, most relevant first. Return an empty array [] if none are relevant.")
	return b.String()
}

// parseNodeIDs extracts a JSON array of string IDs from the LLM response,
// tolerating markdown code fences.
func parseNodeIDs(content string) ([]string, error) {
	cleaned := strings.TrimSpace(content)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var ids []string
	if err := json.Unmarshal([]byte(cleaned), &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
