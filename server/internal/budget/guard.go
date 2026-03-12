// Package budget enforces per-user daily token budgets and RPM rate limits.
package budget

import (
	"errors"
	"sync"
	"time"
)

// ErrDailyBudgetExceeded is returned when a user's daily token budget is spent.
var ErrDailyBudgetExceeded = errors.New("daily token budget exceeded")

// ErrRateLimited is returned when a user exceeds the requests-per-minute limit.
var ErrRateLimited = errors.New("rate limit exceeded")

// UsageQuerier fetches the total tokens consumed today for a given tier/user.
type UsageQuerier interface {
	TodayTokenUsage(tier, userID string) (int64, error)
}

// Limits holds the configured budget and rate constraints.
type Limits struct {
	DailyBudgetStandard int
	DailyBudgetCheap    int
	StandardModelRPM    int
	CheapModelRPM       int
}

// Guard checks daily budgets and RPM limits before allowing a chat request.
type Guard struct {
	usage  UsageQuerier
	limits Limits

	mu      sync.Mutex
	windows map[string]*slidingWindow // key: userID + ":" + tier
}

// NewGuard creates a budget guard with the given usage backend and limits.
func NewGuard(usage UsageQuerier, limits Limits) *Guard {
	return &Guard{
		usage:   usage,
		limits:  limits,
		windows: make(map[string]*slidingWindow),
	}
}

// SetLimits hot-swaps the budget and rate limits (e.g. after a config change).
func (g *Guard) SetLimits(l Limits) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.limits = l
}

// Allow checks whether a request for the given tier and user is permitted.
// An empty userID means single-user mode; limits are applied globally.
func (g *Guard) Allow(tier, userID string) error {
	g.mu.Lock()
	limits := g.limits
	g.mu.Unlock()

	// RPM check (in-memory, fast path).
	rpm := limits.StandardModelRPM
	if tier == "cheap" {
		rpm = limits.CheapModelRPM
	}
	if rpm > 0 {
		key := userID + ":" + tier
		if !g.recordRequest(key, rpm) {
			return ErrRateLimited
		}
	}

	// Daily budget check (DB query, slower path).
	budget := limits.DailyBudgetStandard
	if tier == "cheap" {
		budget = limits.DailyBudgetCheap
	}
	if budget > 0 && g.usage != nil {
		used, err := g.usage.TodayTokenUsage(tier, userID)
		if err != nil {
			// If the query fails, allow the request rather than blocking users.
			return nil
		}
		if used >= int64(budget) {
			return ErrDailyBudgetExceeded
		}
	}

	return nil
}

// recordRequest returns true if the request is within the RPM limit.
func (g *Guard) recordRequest(key string, limit int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	w, ok := g.windows[key]
	if !ok {
		w = &slidingWindow{}
		g.windows[key] = w
	}
	return w.allow(limit)
}

// slidingWindow tracks request timestamps for a one-minute sliding window.
type slidingWindow struct {
	timestamps []time.Time
}

func (w *slidingWindow) allow(limit int) bool {
	now := time.Now()
	cutoff := now.Add(-time.Minute)

	// Trim expired entries.
	start := 0
	for start < len(w.timestamps) && w.timestamps[start].Before(cutoff) {
		start++
	}
	w.timestamps = w.timestamps[start:]

	if len(w.timestamps) >= limit {
		return false
	}
	w.timestamps = append(w.timestamps, now)
	return true
}
