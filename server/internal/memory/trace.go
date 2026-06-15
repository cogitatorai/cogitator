package memory

import (
	"context"
	"sync"
)

// RetrievalTrace is the per-turn diagnostic record. Metadata only: no memory
// body text and no query text are stored.
type RetrievalTrace struct {
	RequestID   string           `json:"request_id"`
	SessionKey  string           `json:"session_key"`
	UserID      string           `json:"user_id"`
	Path        string           `json:"path"` // "vector" | "llm-fallback"
	QueryChars  int              `json:"query_chars"`
	Budget      int              `json:"budget"`
	TokensUsed  int              `json:"tokens_used"`
	Candidates  []TraceCandidate `json:"candidates"`
	InjectedIDs []string         `json:"injected_ids"`
	PinnedIDs   []string         `json:"pinned_ids"`
}

// TraceCandidate is one scored candidate node and its disposition.
type TraceCandidate struct {
	NodeID     string   `json:"node_id"`
	Title      string   `json:"title,omitempty"` // set only for nodes we already loaded
	Type       NodeType `json:"type"`
	Similarity float64  `json:"similarity"` // raw cosine (vector path; 0 on llm path)
	Score      float64  `json:"score"`      // sim * confidence * typeBoost
	EstTokens  int      `json:"est_tokens"`
	Injected   bool     `json:"injected"`
	DropReason string   `json:"drop_reason,omitempty"`
}

// Drop reason constants.
const (
	DropBelowMinSimilarity = "below_min_similarity"
	DropTokenBudget        = "token_budget"
	DropOutsideTopK        = "outside_top_k"
	DropLLMNotSelected     = "llm_not_selected"
)

// TraceSink receives completed traces (implemented by *TraceRing).
type TraceSink interface {
	Record(*RetrievalTrace)
}

// StatsSink receives per-turn aggregate samples (implemented by
// *metrics.RetrievalStats). Defined here so internal/memory does not import
// internal/metrics.
type StatsSink interface {
	Record(topSim float64, injected int, budgetUtil float64)
}

// TraceRing is a bounded, thread-safe ring of the most recent traces.
type TraceRing struct {
	mu      sync.Mutex
	size    int
	pos     int
	full    bool
	entries []*RetrievalTrace
}

// NewTraceRing creates a ring holding the last size traces.
func NewTraceRing(size int) *TraceRing {
	if size <= 0 {
		size = 200
	}
	return &TraceRing{size: size, entries: make([]*RetrievalTrace, size)}
}

// Record stores a trace, evicting the oldest when full.
func (r *TraceRing) Record(t *RetrievalTrace) {
	if t == nil {
		return
	}
	r.mu.Lock()
	r.entries[r.pos] = t
	r.pos++
	if r.pos >= r.size {
		r.pos = 0
		r.full = true
	}
	r.mu.Unlock()
}

// Snapshot returns the stored traces, newest first.
func (r *TraceRing) Snapshot() []*RetrievalTrace {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.pos
	if r.full {
		n = r.size
	}
	out := make([]*RetrievalTrace, 0, n)
	// Walk backwards from the most recently written slot.
	for i := 0; i < n; i++ {
		idx := (r.pos - 1 - i + r.size) % r.size
		if r.entries[idx] != nil {
			out = append(out, r.entries[idx])
		}
	}
	return out
}

// TraceHolder captures a single trace for inline return (the ?debug path).
type TraceHolder struct {
	mu sync.Mutex
	tr *RetrievalTrace
}

// Set stores the trace.
func (h *TraceHolder) Set(t *RetrievalTrace) {
	h.mu.Lock()
	h.tr = t
	h.mu.Unlock()
}

// Get returns the captured trace, or nil.
func (h *TraceHolder) Get() *RetrievalTrace {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.tr
}

type traceCtxKey int

const traceHolderKey traceCtxKey = iota

// WithTrace plants a TraceHolder in the context, requesting that the retriever
// build a trace for this turn even when the global trace flag is off. Returns
// the new context and the holder to read after retrieval.
func WithTrace(ctx context.Context) (context.Context, *TraceHolder) {
	h := &TraceHolder{}
	return context.WithValue(ctx, traceHolderKey, h), h
}

// traceHolderFrom returns the planted holder, or nil.
func traceHolderFrom(ctx context.Context) *TraceHolder {
	h, _ := ctx.Value(traceHolderKey).(*TraceHolder)
	return h
}
