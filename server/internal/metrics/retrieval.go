package metrics

import (
	"math"
	"sort"
	"sync"
)

// RetrievalStats aggregates per-turn memory-retrieval signals. Turn and
// zero-retrieval counts are cumulative; the distribution stats (top similarity,
// injected count, budget utilization) are computed over a bounded ring of the
// most recent samples. All values are numeric — no memory content — so this is
// always-on. Only the vector retrieval path records here (similarity is
// meaningless on the llm-fallback path).
type RetrievalStats struct {
	mu         sync.Mutex
	size       int
	pos        int
	full       bool
	topSim     []float64
	injected   []int
	budgetUtil []float64
	turns      int
	zero       int
}

// NewRetrievalStats creates a stats aggregator keeping the last size samples.
func NewRetrievalStats(size int) *RetrievalStats {
	if size <= 0 {
		size = 1000
	}
	return &RetrievalStats{
		size:       size,
		topSim:     make([]float64, size),
		injected:   make([]int, size),
		budgetUtil: make([]float64, size),
	}
}

// Record adds one retrieval turn. topSim is the highest candidate cosine
// similarity seen that turn (regardless of threshold), injected is the number
// of nodes injected, budgetUtil is tokensUsed/budget in [0,1].
func (s *RetrievalStats) Record(topSim float64, injected int, budgetUtil float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turns++
	if injected == 0 {
		s.zero++
	}
	s.topSim[s.pos] = topSim
	s.injected[s.pos] = injected
	s.budgetUtil[s.pos] = budgetUtil
	s.pos++
	if s.pos >= s.size {
		s.pos = 0
		s.full = true
	}
}

// Percentiles holds a small distribution summary.
type Percentiles struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	Avg float64 `json:"avg"`
}

// RetrievalSnapshot is the JSON-serializable view surfaced in /api/status.
type RetrievalSnapshot struct {
	Turns             int         `json:"turns"`
	ZeroRetrievalRate float64     `json:"zero_retrieval_rate"`
	TopSimilarity     Percentiles `json:"top_similarity"`
	AvgInjected       float64     `json:"avg_injected"`
	AvgBudgetUtil     float64     `json:"avg_budget_util"`
}

// Snapshot returns a point-in-time summary. Cumulative counters cover all turns;
// distribution stats cover the recent sample ring.
func (s *RetrievalStats) Snapshot() RetrievalSnapshot {
	s.mu.Lock()
	n := s.pos
	if s.full {
		n = s.size
	}
	turns := s.turns
	zero := s.zero
	sims := make([]float64, n)
	var sumInj, sumBudget float64
	for i := 0; i < n; i++ {
		sims[i] = s.topSim[i]
		sumInj += float64(s.injected[i])
		sumBudget += s.budgetUtil[i]
	}
	s.mu.Unlock()

	snap := RetrievalSnapshot{Turns: turns}
	if turns > 0 {
		snap.ZeroRetrievalRate = float64(zero) / float64(turns)
	}
	if n == 0 {
		return snap
	}
	sorted := make([]float64, n)
	copy(sorted, sims)
	sort.Float64s(sorted)
	var sumSim float64
	for _, v := range sorted {
		sumSim += v
	}
	snap.TopSimilarity = Percentiles{
		P50: sorted[pctIndex(n, 0.50)],
		P95: sorted[pctIndex(n, 0.95)],
		Avg: sumSim / float64(n),
	}
	snap.AvgInjected = sumInj / float64(n)
	snap.AvgBudgetUtil = sumBudget / float64(n)
	return snap
}

// pctIndex returns the index of the p-th percentile in a sorted slice of n items.
func pctIndex(n int, p float64) int {
	idx := int(math.Ceil(float64(n)*p)) - 1
	if idx < 0 {
		return 0
	}
	if idx >= n {
		return n - 1
	}
	return idx
}
