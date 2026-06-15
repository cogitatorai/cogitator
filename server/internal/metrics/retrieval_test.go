package metrics

import "testing"

func TestRetrievalStatsSnapshot(t *testing.T) {
	s := NewRetrievalStats(10)
	// topSim, injected, budgetUtil
	s.Record(0.9, 3, 0.5)
	s.Record(0.5, 0, 0.0) // zero-retrieval turn
	s.Record(0.7, 2, 1.0)

	snap := s.Snapshot()
	if snap.Turns != 3 {
		t.Errorf("Turns = %d, want 3", snap.Turns)
	}
	wantZero := 1.0 / 3.0
	if snap.ZeroRetrievalRate < wantZero-0.001 || snap.ZeroRetrievalRate > wantZero+0.001 {
		t.Errorf("ZeroRetrievalRate = %v, want ~%v", snap.ZeroRetrievalRate, wantZero)
	}
	if snap.TopSimilarity.Avg < 0.69 || snap.TopSimilarity.Avg > 0.71 {
		t.Errorf("TopSimilarity.Avg = %v, want ~0.70", snap.TopSimilarity.Avg)
	}
	if snap.TopSimilarity.P95 < 0.89 || snap.TopSimilarity.P95 > 0.91 {
		t.Errorf("TopSimilarity.P95 = %v, want ~0.90", snap.TopSimilarity.P95)
	}
	if snap.AvgInjected < 1.66 || snap.AvgInjected > 1.67 {
		t.Errorf("AvgInjected = %v, want ~1.667", snap.AvgInjected)
	}
	if snap.AvgBudgetUtil < 0.49 || snap.AvgBudgetUtil > 0.51 {
		t.Errorf("AvgBudgetUtil = %v, want ~0.50", snap.AvgBudgetUtil)
	}
}

func TestRetrievalStatsEmpty(t *testing.T) {
	s := NewRetrievalStats(10)
	snap := s.Snapshot()
	if snap.Turns != 0 || snap.ZeroRetrievalRate != 0 || snap.TopSimilarity.Avg != 0 {
		t.Errorf("empty snapshot non-zero: %+v", snap)
	}
}
