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
			r.Record(time.Duration(i+1)*time.Millisecond, status)
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
			r2.Record(time.Millisecond, 200)
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
					r3.Record(time.Millisecond, 200)
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
