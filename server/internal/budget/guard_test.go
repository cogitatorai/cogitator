package budget

import (
	"testing"
	"time"
)

// stubQuerier returns a fixed value for any query.
type stubQuerier struct {
	used int64
	err  error
}

func (s *stubQuerier) TodayTokenUsage(_, _ string) (int64, error) {
	return s.used, s.err
}

func TestAllow_UnderBudget(t *testing.T) {
	g := NewGuard(&stubQuerier{used: 100}, Limits{
		DailyBudgetStandard: 1000,
	})
	if err := g.Allow("standard", "u1"); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAllow_BudgetExceeded(t *testing.T) {
	g := NewGuard(&stubQuerier{used: 1000}, Limits{
		DailyBudgetStandard: 1000,
	})
	if err := g.Allow("standard", "u1"); err != ErrDailyBudgetExceeded {
		t.Fatalf("expected ErrDailyBudgetExceeded, got %v", err)
	}
}

func TestAllow_CheapTier(t *testing.T) {
	g := NewGuard(&stubQuerier{used: 500}, Limits{
		DailyBudgetCheap: 400,
	})
	if err := g.Allow("cheap", "u1"); err != ErrDailyBudgetExceeded {
		t.Fatalf("expected ErrDailyBudgetExceeded for cheap tier, got %v", err)
	}
}

func TestAllow_ZeroBudget_NoEnforcement(t *testing.T) {
	g := NewGuard(&stubQuerier{used: 999999}, Limits{
		DailyBudgetStandard: 0,
	})
	if err := g.Allow("standard", "u1"); err != nil {
		t.Fatalf("zero budget should disable enforcement, got %v", err)
	}
}

func TestAllow_NilQuerier_NoEnforcement(t *testing.T) {
	g := NewGuard(nil, Limits{
		DailyBudgetStandard: 1000,
	})
	if err := g.Allow("standard", "u1"); err != nil {
		t.Fatalf("nil querier should disable budget enforcement, got %v", err)
	}
}

func TestAllow_RPM_WithinLimit(t *testing.T) {
	g := NewGuard(nil, Limits{StandardModelRPM: 5})
	for i := 0; i < 5; i++ {
		if err := g.Allow("standard", "u1"); err != nil {
			t.Fatalf("request %d should be allowed, got %v", i, err)
		}
	}
}

func TestAllow_RPM_Exceeded(t *testing.T) {
	g := NewGuard(nil, Limits{StandardModelRPM: 3})
	for i := 0; i < 3; i++ {
		if err := g.Allow("standard", "u1"); err != nil {
			t.Fatalf("request %d should be allowed, got %v", i, err)
		}
	}
	if err := g.Allow("standard", "u1"); err != ErrRateLimited {
		t.Fatalf("4th request should be rate limited, got %v", err)
	}
}

func TestAllow_RPM_PerUser(t *testing.T) {
	g := NewGuard(nil, Limits{StandardModelRPM: 2})
	// User 1 exhausts their limit.
	g.Allow("standard", "u1")
	g.Allow("standard", "u1")
	if err := g.Allow("standard", "u1"); err != ErrRateLimited {
		t.Fatalf("u1 should be rate limited, got %v", err)
	}
	// User 2 should still have capacity.
	if err := g.Allow("standard", "u2"); err != nil {
		t.Fatalf("u2 should be allowed, got %v", err)
	}
}

func TestAllow_RPM_ZeroDisablesEnforcement(t *testing.T) {
	g := NewGuard(nil, Limits{StandardModelRPM: 0})
	for i := 0; i < 100; i++ {
		if err := g.Allow("standard", "u1"); err != nil {
			t.Fatalf("zero RPM should disable rate limiting, got %v", err)
		}
	}
}

func TestSetLimits(t *testing.T) {
	g := NewGuard(&stubQuerier{used: 500}, Limits{
		DailyBudgetStandard: 1000,
	})
	if err := g.Allow("standard", "u1"); err != nil {
		t.Fatalf("should be under budget, got %v", err)
	}
	g.SetLimits(Limits{DailyBudgetStandard: 400})
	if err := g.Allow("standard", "u1"); err != ErrDailyBudgetExceeded {
		t.Fatalf("should exceed new budget, got %v", err)
	}
}

func TestSlidingWindow_Expiry(t *testing.T) {
	w := &slidingWindow{}
	// Fill the window.
	w.timestamps = []time.Time{
		time.Now().Add(-61 * time.Second), // expired
		time.Now().Add(-61 * time.Second), // expired
	}
	// Should be allowed because expired entries are trimmed.
	if !w.allow(2) {
		t.Fatal("expired entries should be trimmed, request should be allowed")
	}
}
