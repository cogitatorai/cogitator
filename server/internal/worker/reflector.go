package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/memory"
	"github.com/cogitatorai/cogitator/server/internal/provider"
	"github.com/cogitatorai/cogitator/server/internal/session"
)

const (
	defaultMessageWindow        = 20
	acknowledgmentMinConfidence = 0.85
)

// Reflector subscribes to reflection.triggered events, reviews recent
// conversation messages, and classifies behavioral signals using a cheap LLM.
// For each correction or refinement signal, it creates an episode node and
// queues enrichment. Acknowledgments above a confidence threshold also produce
// episode nodes. The worker has zero latency impact on the conversation path.
type Reflector struct {
	sessions  *session.Store
	memory    *memory.Store
	content   *memory.ContentManager
	provider  provider.Provider
	eventBus  *bus.Bus
	model     string
	msgWindow int
	logger    *slog.Logger
	cancel    context.CancelFunc

	mu sync.Mutex
}

// NewReflector constructs a Reflector. Pass msgWindow <= 0 to use the default
// of 20 messages.
func NewReflector(
	sessions *session.Store,
	mem *memory.Store,
	content *memory.ContentManager,
	prov provider.Provider,
	eventBus *bus.Bus,
	model string,
	msgWindow int,
	logger *slog.Logger,
) *Reflector {
	if logger == nil {
		logger = slog.Default()
	}
	if msgWindow <= 0 {
		msgWindow = defaultMessageWindow
	}
	return &Reflector{
		sessions:  sessions,
		memory:    mem,
		content:   content,
		provider:  prov,
		eventBus:  eventBus,
		model:     model,
		msgWindow: msgWindow,
		logger:    logger,
	}
}

func (r *Reflector) Start(ctx context.Context) {
	ctx, r.cancel = context.WithCancel(ctx)
	ch := r.eventBus.Subscribe(bus.ReflectionTriggered)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-ch:
				if !ok {
					return
				}
				r.handleEvent(ctx, evt)
			}
		}
	}()

	r.logger.Info("reflector started")
}

func (r *Reflector) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
}

// SetProvider hot-swaps the LLM provider and model used for reflection.
func (r *Reflector) SetProvider(p provider.Provider, model string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.provider = p
	r.model = model
}

// reflectionSignal is one classified behavioral signal from the LLM.
type reflectionSignal struct {
	Type         string  `json:"type"`          // correction | refinement | acknowledgment
	Summary      string  `json:"summary"`
	SuggestedRule string `json:"suggested_rule"`
	Confidence   float64 `json:"confidence"`
	MessageIndex int     `json:"message_index"`
}

// reflectionResponse is the full JSON structure the LLM returns.
type reflectionResponse struct {
	Signals []reflectionSignal `json:"signals"`
}

func (r *Reflector) handleEvent(ctx context.Context, evt bus.Event) {
	sessionKey, ok := evt.Payload["session_key"].(string)
	if !ok || sessionKey == "" {
		r.logger.Warn("reflection.triggered event missing session_key")
		return
	}

	// Determine ownership: if the session is private, nodes go to the user's
	// private graph; otherwise they are shared.
	var ownerID *string
	if sess, err := r.sessions.GetByKey(sessionKey); err == nil && sess.Private && sess.UserID != "" {
		uid := sess.UserID
		ownerID = &uid
	}

	msgs, err := r.sessions.GetMessages(sessionKey, r.msgWindow)
	if err != nil {
		r.logger.Error("failed to load messages for reflection",
			"session_key", sessionKey, "error", err)
		return
	}
	if len(msgs) == 0 {
		return
	}

	signals, err := r.classify(ctx, msgs)
	if err != nil {
		r.logger.Error("signal classification failed",
			"session_key", sessionKey, "error", err)
		return
	}
	if len(signals) == 0 {
		r.logger.Debug("no behavioral signals found", "session_key", sessionKey)
		return
	}

	// Load existing preference and pattern nodes so we can wire edges from
	// each new episode to any directly relevant existing node.
	existingNodes, _ := r.memory.GetNodeSummaries("", memory.NodePreference, memory.NodePattern)

	created := 0
	for _, sig := range signals {
		if !r.shouldStore(sig) {
			continue
		}
		nodeID, err := r.storeSignal(sig, sessionKey, ownerID, existingNodes)
		if err != nil {
			r.logger.Error("failed to store signal",
				"session_key", sessionKey, "type", sig.Type, "error", err)
			continue
		}
		r.eventBus.Publish(bus.Event{
			Type:      bus.EnrichmentQueued,
			Payload:   map[string]any{"node_id": nodeID},
			Timestamp: time.Now(),
		})
		created++
	}

	if created > 0 {
		r.logger.Info("reflection complete",
			"session_key", sessionKey,
			"signals_found", len(signals),
			"nodes_created", created,
		)
	}
}

// shouldStore returns true when a signal warrants creating an episode node.
func (r *Reflector) shouldStore(sig reflectionSignal) bool {
	switch sig.Type {
	case "correction", "refinement":
		return true
	case "acknowledgment":
		return sig.Confidence >= acknowledgmentMinConfidence
	}
	return false
}

// classify sends the conversation messages to the LLM and parses the response.
func (r *Reflector) classify(ctx context.Context, msgs []session.Message) ([]reflectionSignal, error) {
	r.mu.Lock()
	prov, model := r.provider, r.model
	r.mu.Unlock()
	if prov == nil {
		return nil, fmt.Errorf("no provider configured")
	}

	prompt := buildReflectionPrompt(msgs)

	provMsgs := []provider.Message{
		{
			Role:    "system",
			Content: "You are a behavioral signal classifier. Respond ONLY with valid JSON.",
		},
		{Role: "user", Content: prompt},
	}

	resp, err := prov.Chat(ctx, provMsgs, nil, model, nil)
	if err != nil {
		return nil, err
	}

	var result reflectionResponse
	raw := resp.Content
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		// Strip markdown code fences if the model wrapped the JSON.
		cleaned := strings.TrimSpace(raw)
		cleaned = strings.TrimPrefix(cleaned, "```json")
		cleaned = strings.TrimPrefix(cleaned, "```")
		cleaned = strings.TrimSuffix(cleaned, "```")
		cleaned = strings.TrimSpace(cleaned)
		if err2 := json.Unmarshal([]byte(cleaned), &result); err2 != nil {
			return nil, fmt.Errorf("parse reflection response: %w", err2)
		}
	}

	return result.Signals, nil
}

// storeSignal creates an episode node for the signal and wires edges to any
// directly relevant existing preference or pattern nodes. The ownerID
// determines whether the node and its edges are private (non-nil) or shared
// (nil).
func (r *Reflector) storeSignal(sig reflectionSignal, sessionKey string, ownerID *string, existing []memory.NodeSummary) (string, error) {
	title := titleForSignal(sig)

	node := &memory.Node{
		Type:       memory.NodeEpisode,
		UserID:     ownerID,
		Title:      title,
		Summary:    sig.Summary,
		Confidence: sig.Confidence,
		Origin:     "reflection:" + sessionKey,
		Tags:       []string{"behavioral-signal", sig.Type},
	}

	if sig.SuggestedRule != "" {
		node.RetrievalTriggers = []string{sig.SuggestedRule}
	}

	nodeID, err := r.memory.CreateNode(node)
	if err != nil {
		return "", err
	}

	// Write full content to disk so the enricher has rich material.
	if r.content != nil {
		content := buildEpisodeContent(sig, sessionKey)
		relPath, writeErr := r.content.Write(nodeID, content)
		if writeErr == nil {
			node.ID = nodeID
			node.ContentPath = relPath
			_ = r.memory.UpdateNode(node)
			_ = r.memory.UpdateContentLength(nodeID, len(content))
		}
	}

	// Wire edges to closely matching existing nodes on a best-effort basis.
	// Deep graph enrichment happens in the Enricher; here we only wire edges
	// when we have high confidence from a keyword match.
	now := time.Now()
	for _, ns := range existing {
		rel := edgeRelation(sig.Type)
		if rel == "" {
			continue
		}
		if nodesSeem(sig, ns) {
			_ = r.memory.CreateEdge(&memory.Edge{
				SourceID:  nodeID,
				TargetID:  ns.ID,
				UserID:    ownerID,
				Relation:  rel,
				Weight:    sig.Confidence,
				CreatedAt: now,
			})
		}
	}

	return nodeID, nil
}

// edgeRelation returns the appropriate relation type for the given signal type.
func edgeRelation(sigType string) memory.RelationType {
	switch sigType {
	case "correction":
		return memory.RelRefines
	case "refinement":
		return memory.RelRefines
	case "acknowledgment":
		return memory.RelSupports
	}
	return ""
}

// nodesSeem is a lightweight heuristic: check if any word from the signal
// summary or suggested rule appears in the existing node title or summary.
// The Enricher will do rigorous semantic matching; this is opportunistic.
func nodesSeem(sig reflectionSignal, ns memory.NodeSummary) bool {
	haystack := strings.ToLower(ns.Title + " " + ns.Summary)
	for _, trigger := range ns.RetrievalTriggers {
		haystack += " " + strings.ToLower(trigger)
	}

	needle := strings.ToLower(sig.Summary + " " + sig.SuggestedRule)
	words := strings.Fields(needle)
	matches := 0
	for _, w := range words {
		if len(w) > 4 && strings.Contains(haystack, w) {
			matches++
		}
	}
	return matches >= 2
}

func titleForSignal(sig reflectionSignal) string {
	prefix := ""
	switch sig.Type {
	case "correction":
		prefix = "Correction: "
	case "refinement":
		prefix = "Refinement: "
	case "acknowledgment":
		prefix = "Acknowledgment: "
	default:
		prefix = "Signal: "
	}
	title := sig.Summary
	if len(title) > 80 {
		title = title[:77] + "..."
	}
	return prefix + title
}

func buildReflectionPrompt(msgs []session.Message) string {
	var b strings.Builder
	b.WriteString("Review the following conversation and classify each agent (assistant) response.\n")
	b.WriteString("For each response where the user expressed dissatisfaction, corrected the output,\n")
	b.WriteString("redirected the approach, or acknowledged something particularly helpful,\n")
	b.WriteString("extract the behavioral signal.\n\n")
	b.WriteString("Conversation:\n")

	for i, m := range msgs {
		role := m.Role
		if role == "" {
			role = "unknown"
		}
		b.WriteString(fmt.Sprintf("[%d] %s: %s\n", i, role, m.Content))
	}

	b.WriteString(`
Respond with a JSON object:
{
  "signals": [
    {
      "type": "correction|refinement|acknowledgment",
      "summary": "concise description of what the user preferred",
      "suggested_rule": "actionable rule to apply in future responses",
      "confidence": 0.0-1.0,
      "message_index": <index of the user message that contains the signal>
    }
  ]
}

If no signals are found, return {"signals": []}.
Only include corrections (user disliked output), refinements (user redirected approach),
and acknowledgments (user praised or confirmed something was exactly right).`)

	return b.String()
}

func buildEpisodeContent(sig reflectionSignal, sessionKey string) string {
	var b strings.Builder
	b.WriteString("# Behavioral Signal\n\n")
	b.WriteString(fmt.Sprintf("**Type:** %s\n\n", sig.Type))
	b.WriteString(fmt.Sprintf("**Session:** %s\n\n", sessionKey))
	b.WriteString(fmt.Sprintf("**Summary:** %s\n\n", sig.Summary))
	if sig.SuggestedRule != "" {
		b.WriteString(fmt.Sprintf("**Suggested Rule:** %s\n\n", sig.SuggestedRule))
	}
	b.WriteString(fmt.Sprintf("**Confidence:** %.2f\n", sig.Confidence))
	return b.String()
}
