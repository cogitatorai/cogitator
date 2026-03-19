package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/memory"
	"github.com/cogitatorai/cogitator/server/internal/provider"
)

const (
	// enrichWorkers is the number of concurrent enrichment goroutines.
	enrichWorkers = 3
	// enrichDebounce is how long we wait after an event before querying the
	// DB, so rapid-fire events are coalesced into a single batch.
	enrichDebounce = 500 * time.Millisecond
)

// Enricher subscribes to enrichment.queued events and enriches pending memory
// nodes by calling an LLM to generate summaries, tags, retrieval triggers, and
// graph edges. It runs a bounded worker pool for parallel enrichment.
type Enricher struct {
	memory       *memory.Store
	content      *memory.ContentManager
	mu           sync.RWMutex
	provider     provider.Provider
	eventBus     *bus.Bus
	model        string
	logger       *slog.Logger
	cancel       context.CancelFunc
	nodeEmbedder *memory.NodeEmbedder
	retriever    *memory.Retriever
	active       atomic.Int32
	userNames    []string
}

// IsActive reports whether the enricher is currently processing nodes.
func (e *Enricher) IsActive() bool {
	return e.active.Load() > 0
}

func NewEnricher(
	mem *memory.Store,
	content *memory.ContentManager,
	prov provider.Provider,
	eventBus *bus.Bus,
	model string,
	logger *slog.Logger,
	nodeEmbedder *memory.NodeEmbedder,
	retriever *memory.Retriever,
) *Enricher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Enricher{
		memory:       mem,
		content:      content,
		provider:     prov,
		eventBus:     eventBus,
		model:        model,
		logger:       logger,
		nodeEmbedder: nodeEmbedder,
		retriever:    retriever,
	}
}

// SetUserNames sets the list of known user names used to sanitize LLM summaries.
func (e *Enricher) SetUserNames(names []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.userNames = names
}

// SetRetriever sets the retriever used to invalidate cache after embedding.
func (e *Enricher) SetRetriever(r *memory.Retriever) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.retriever = r
}

// SetProvider hot-swaps the LLM provider and model used for enrichment.
// If there are pending nodes, it publishes an EnrichmentQueued event to
// trigger processing.
func (e *Enricher) SetProvider(p provider.Provider, model string) {
	e.mu.Lock()
	e.provider = p
	if model != "" {
		e.model = model
	}
	e.mu.Unlock()

	// Kick the enricher to process any pending nodes now that a provider is available.
	if p != nil && e.eventBus != nil {
		e.eventBus.Publish(bus.Event{Type: bus.EnrichmentQueued})
	}
}

func (e *Enricher) Start(ctx context.Context) {
	ctx, e.cancel = context.WithCancel(ctx)
	ch := e.eventBus.Subscribe(bus.EnrichmentQueued)
	ticker := time.NewTicker(5 * time.Minute)

	// Kick off immediately to drain any pending nodes left over from a
	// previous crash or restart.
	kickCh := make(chan struct{}, 1)
	kickCh <- struct{}{}

	go func() {
		defer ticker.Stop()

		var debounce *time.Timer
		for {
			select {
			case <-ctx.Done():
				return
			case <-kickCh:
				e.processPending(ctx)
			case _, ok := <-ch:
				if !ok {
					return
				}
				// Debounce rapid events: reset the timer each time so we
				// coalesce bursts into a single processPending call.
				if debounce == nil {
					debounce = time.AfterFunc(enrichDebounce, func() {
						e.processPending(ctx)
					})
				} else {
					debounce.Reset(enrichDebounce)
				}
			case <-ticker.C:
				e.processPending(ctx)
			}
		}
	}()

	e.logger.Info("enricher started")
}

func (e *Enricher) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
}

// processPending fetches all pending nodes and enriches them using a bounded
// worker pool. It keeps fetching batches until no pending nodes remain.
func (e *Enricher) processPending(ctx context.Context) {
	p, _ := e.getProvider()
	if p == nil {
		e.logger.Warn("enrichment skipped: no provider configured")
		return
	}

	e.active.Add(1)
	defer e.active.Add(-1)

	for {
		if ctx.Err() != nil {
			return
		}

		nodes, err := e.memory.GetPendingEnrichment(enrichWorkers * 2)
		if err != nil {
			e.logger.Error("failed to get pending nodes", "error", err)
			return
		}
		if len(nodes) == 0 {
			return
		}

		var wg sync.WaitGroup
		sem := make(chan struct{}, enrichWorkers)

		for _, node := range nodes {
			if ctx.Err() != nil {
				break
			}
			wg.Add(1)
			sem <- struct{}{} // acquire slot
			go func(n memory.Node) {
				defer wg.Done()
				defer func() { <-sem }() // release slot
				if err := e.enrichNode(ctx, n); err != nil {
					e.logger.Error("enrichment failed", "node_id", n.ID, "error", err)
				}
			}(node)
		}
		wg.Wait()
	}
}

// enrichResult is the structured response expected from the LLM.
type enrichResult struct {
	NodeType          string   `json:"node_type"`
	Summary           string   `json:"summary"`
	Tags              []string `json:"tags"`
	RetrievalTriggers []string `json:"retrieval_triggers"`
	RelatedNodes      []struct {
		ID       string  `json:"id"`
		Relation string  `json:"relation"`
		Weight   float64 `json:"weight"`
	} `json:"related_nodes"`
	Contradictions []string `json:"contradictions"`
}

func (e *Enricher) getProvider() (provider.Provider, string) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.provider, e.model
}

func (e *Enricher) enrichNode(ctx context.Context, node memory.Node) error {
	// Load content from disk when a content path is recorded.
	content := ""
	if node.ContentPath != "" && e.content != nil {
		if c, err := e.content.Read(node.ContentPath); err == nil {
			content = c
		}
	}

	// Build a compact snapshot of the existing graph for relation discovery.
	// Scope to the node owner's visibility: shared nodes + their private nodes.
	scopeUID := ""
	if node.UserID != nil {
		scopeUID = *node.UserID
	}
	summaries, _ := e.memory.GetNodeSummaries(scopeUID)
	summaryBlock := buildSummaryBlock(summaries, node.ID)

	prompt := buildEnrichmentPrompt(node, content, summaryBlock)

	messages := []provider.Message{
		{
			Role:    "system",
			Content: "You are a knowledge graph enrichment agent. Respond ONLY with valid JSON.",
		},
		{Role: "user", Content: prompt},
	}

	p, model := e.getProvider()
	if p == nil {
		return fmt.Errorf("no provider configured")
	}
	resp, err := p.Chat(ctx, messages, nil, model, nil)
	if err != nil {
		return err
	}

	var result enrichResult
	raw := resp.Content
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		// Strip markdown code fences if the model wrapped the JSON.
		cleaned := strings.TrimSpace(raw)
		cleaned = strings.TrimPrefix(cleaned, "```json")
		cleaned = strings.TrimPrefix(cleaned, "```")
		cleaned = strings.TrimSuffix(cleaned, "```")
		cleaned = strings.TrimSpace(cleaned)
		if err2 := json.Unmarshal([]byte(cleaned), &result); err2 != nil {
			return err2
		}
	}

	// Run server-side validation to sanitize and normalise the LLM output.
	e.mu.RLock()
	names := e.userNames
	e.mu.RUnlock()

	validated := memory.ValidateEnrichmentResult(
		result.NodeType, result.Summary,
		result.Tags, result.RetrievalTriggers,
		names, content,
	)

	node.Type = validated.NodeType
	node.Summary = validated.Summary
	node.Tags = validated.Tags
	node.RetrievalTriggers = validated.Triggers
	node.EnrichmentStatus = memory.EnrichmentComplete

	if err := e.memory.UpdateNode(&node); err != nil {
		return err
	}

	if e.nodeEmbedder != nil {
		if enrichedNode, err := e.memory.GetNode(node.ID); err == nil && enrichedNode != nil {
			if err := e.nodeEmbedder.EmbedNode(ctx, enrichedNode); err != nil {
				e.logger.Warn("re-embed after enrichment failed", "node_id", node.ID, "error", err)
			}
			if e.retriever != nil {
				e.retriever.InvalidateCache()
			}
		}
	}

	now := time.Now()

	for _, rel := range result.RelatedNodes {
		targetNode, err := e.memory.GetNode(rel.ID)
		if err != nil || targetNode == nil {
			continue
		}

		relType := memory.RelationType(rel.Relation)
		switch relType {
		case memory.RelRefines, memory.RelContradicts, memory.RelSupports,
			memory.RelDerivedFrom, memory.RelExampleOf, memory.RelRelatedTo:
			// valid relation type
		default:
			continue
		}

		// Derive edge weight from embedding similarity rather than the
		// LLM-proposed value, which is unreliable.
		srcEmb, _ := e.memory.GetEmbedding(node.ID)
		tgtEmb, _ := e.memory.GetEmbedding(rel.ID)
		w := 0.5
		if srcEmb != nil && tgtEmb != nil {
			w = memory.CosineSimilarity(srcEmb, tgtEmb)
		}

		// Ignore edge creation errors: best-effort graph wiring.
		_ = e.memory.CreateEdge(&memory.Edge{
			SourceID:  node.ID,
			TargetID:  rel.ID,
			UserID:    node.UserID,
			Relation:  relType,
			Weight:    w,
			CreatedAt: now,
		})
	}

	for _, contraID := range result.Contradictions {
		if _, err := e.memory.GetNode(contraID); err != nil {
			continue
		}

		// Require embedding similarity >= 0.5 to accept a contradiction.
		// Nodes that are semantically distant are unlikely true contradictions.
		srcEmb, _ := e.memory.GetEmbedding(node.ID)
		tgtEmb, _ := e.memory.GetEmbedding(contraID)
		sim := 0.5
		if srcEmb != nil && tgtEmb != nil {
			sim = memory.CosineSimilarity(srcEmb, tgtEmb)
			if sim < 0.5 {
				continue // drop false contradiction
			}
		}

		_ = e.memory.CreateEdge(&memory.Edge{
			SourceID:  node.ID,
			TargetID:  contraID,
			UserID:    node.UserID,
			Relation:  memory.RelContradicts,
			Weight:    sim,
			CreatedAt: now,
		})

		_ = e.memory.AdjustConfidence(contraID, -0.1, 0.1)
	}

	e.logger.Info("enriched node",
		"node_id", node.ID,
		"tags", len(result.Tags),
		"edges", len(result.RelatedNodes)+len(result.Contradictions),
	)
	return nil
}

func buildEnrichmentPrompt(node memory.Node, content, summaryBlock string) string {
	var b strings.Builder
	b.WriteString("Enrich this memory node.\n\n")
	b.WriteString("Node ID: " + node.ID + "\n")
	b.WriteString("Type: " + string(node.Type) + "\n")
	b.WriteString("Title: " + node.Title + "\n")
	if content != "" {
		b.WriteString("Content:\n" + content + "\n\n")
	}
	if summaryBlock != "" {
		b.WriteString("Existing nodes in the knowledge graph:\n" + summaryBlock + "\n\n")
	}
	b.WriteString(`Respond with a JSON object containing:
- "node_type": Classify this memory as one of: "fact" (objective information), "preference" (subjective likes/dislikes/habits), "pattern" (recurring behavior). Choose based on content, not title.
- "summary": A concise 1-2 sentence summary of this node's content. Do NOT include person names. The system tracks ownership separately. Use "the user" if a person reference is needed
- "tags": Array of 3-7 lowercase tags for categorization
- "retrieval_triggers": Array of up to 100 short phrases or questions that should trigger retrieval of this node. Generate triggers across three categories:
  * Direct: what the node is explicitly about (e.g., topics, names, concepts)
  * Contextual: situations where this knowledge would be useful (e.g., recommendations, decisions, planning)
  * Lateral: cross-domain connections requiring world knowledge (e.g., cultural associations, geographic links, historical parallels). Think beyond obvious associations.
  The number of triggers should reflect the richness of the content. A simple preference may have 20; a complex fact may have 80. Quality matters more than quantity.
- "related_nodes": Array of {id, relation, weight} for related existing nodes. Relation must be one of: refines, contradicts, supports, derived_from, example_of, related_to. Weight is 0.0-1.0.
- "contradictions": Array of node IDs that contradict this node's content`)
	return b.String()
}

func buildSummaryBlock(summaries []memory.NodeSummary, excludeID string) string {
	var b strings.Builder
	for _, s := range summaries {
		if s.ID == excludeID {
			continue
		}
		b.WriteString("- [" + s.ID + "] " + string(s.Type) + ": " + s.Title)
		if s.Summary != "" {
			b.WriteString(" | " + s.Summary)
		}
		b.WriteString("\n")
	}
	return b.String()
}
