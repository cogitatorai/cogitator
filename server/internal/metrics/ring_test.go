package metrics

import (
	"sync"
	"testing"
	"time"
)

func TestRingBuffer(t *testing.T) {
	r := NewRing(100)

	t.Run("empty buffer returns zeros", func(t *testing.T) {
		snap := r.Snapshot()
		if snap.RequestCount != 0 || snap.ErrorRate != 0 || snap.P95LatencyMs != 0 {
			t.Fatalf("expected zeros, got %+v", snap)
		}
	})

	t.Run("records requests and computes stats", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			status := 200
			if i >= 95 {
				status = 500
			}
			r.Record(time.Duration(i+1)*time.Millisecond, status, "GET /test")
		}
		snap := r.Snapshot()
		if snap.RequestCount != 100 {
			t.Fatalf("expected 100 requests, got %d", snap.RequestCount)
		}
		if snap.ErrorRate < 0.04 || snap.ErrorRate > 0.06 {
			t.Fatalf("expected ~5%% error rate, got %.2f", snap.ErrorRate)
		}
		// P95 should be around 95ms (95th item in sorted 1..100)
		if snap.P95LatencyMs < 90 || snap.P95LatencyMs > 100 {
			t.Fatalf("expected p95 ~95ms, got %.1f", snap.P95LatencyMs)
		}
	})

	t.Run("wraps around when full", func(t *testing.T) {
		r2 := NewRing(10)
		for i := 0; i < 20; i++ {
			r2.Record(time.Millisecond, 200, "GET /test")
		}
		snap := r2.Snapshot()
		if snap.RequestCount != 10 {
			t.Fatalf("expected 10 (buffer size), got %d", snap.RequestCount)
		}
	})

	t.Run("concurrent access is safe", func(t *testing.T) {
		r3 := NewRing(1000)
		var wg sync.WaitGroup
		for g := 0; g < 10; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 100; i++ {
					r3.Record(time.Millisecond, 200, "GET /test")
				}
			}()
		}
		// Read snapshots concurrently with writes.
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_ = r3.Snapshot()
			}
		}()
		wg.Wait()
		snap := r3.Snapshot()
		if snap.RequestCount != 1000 {
			t.Fatalf("expected 1000 requests, got %d", snap.RequestCount)
		}
	})
}

func TestSnapshotPerRoute(t *testing.T) {
	r := NewRing(10)
	r.Record(10*time.Millisecond, 200, "GET /api/health")
	r.Record(20*time.Millisecond, 200, "GET /api/health")
	r.Record(30*time.Millisecond, 500, "POST /api/chat")

	snap := r.Snapshot()

	if len(snap.Routes) != 2 {
		t.Fatalf("got %d routes, want 2: %v", len(snap.Routes), snap.Routes)
	}
	health := snap.Routes["GET /api/health"]
	if health.RequestCount != 2 || health.Count5xx != 0 {
		t.Errorf("health route stats = %+v", health)
	}
	if health.P95LatencyMs < 19 || health.P95LatencyMs > 21 {
		t.Errorf("health p95 = %v, want ~20", health.P95LatencyMs)
	}
	chat := snap.Routes["POST /api/chat"]
	if chat.RequestCount != 1 || chat.Count5xx != 1 {
		t.Errorf("chat route stats = %+v", chat)
	}
}

func TestRecordEmptyRouteLabeledUnmatched(t *testing.T) {
	r := NewRing(4)
	r.Record(time.Millisecond, 404, "")
	snap := r.Snapshot()
	if _, ok := snap.Routes["unmatched"]; !ok {
		t.Errorf("empty route not bucketed as unmatched: %v", snap.Routes)
	}
}

func TestSnapshotRoutesAgeOutOnWrap(t *testing.T) {
	r := NewRing(10)
	// Fill the ring with one route, then overwrite every slot with another.
	for i := 0; i < 10; i++ {
		r.Record(time.Millisecond, 200, "GET /old")
	}
	for i := 0; i < 10; i++ {
		r.Record(time.Millisecond, 200, "GET /new")
	}

	snap := r.Snapshot()
	if _, ok := snap.Routes["GET /old"]; ok {
		t.Errorf("overwritten route still present: %v", snap.Routes)
	}
	if got := snap.Routes["GET /new"].RequestCount; got != 10 {
		t.Errorf("surviving route count = %d, want 10", got)
	}
}
