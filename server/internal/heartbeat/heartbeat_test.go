package heartbeat

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/metrics"
)

func TestHeartbeat(t *testing.T) {
	ring := metrics.NewRing(100)
	ring.Record(10*time.Millisecond, 200)

	var received atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		if _, ok := payload["request_count"]; !ok {
			t.Error("missing request_count in heartbeat")
		}
		if r.Header.Get("X-Internal-Secret") != "secret" {
			t.Error("missing or wrong internal secret header")
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	hb := New(Config{
		OrchestratorURL: server.URL,
		TenantID:        "test-tenant",
		InternalSecret:  "secret",
		Ring:            ring,
		Interval:        50 * time.Millisecond, // fast for testing
	})

	hb.Start()
	time.Sleep(200 * time.Millisecond)
	hb.Stop()

	if received.Load() < 2 {
		t.Fatalf("expected at least 2 heartbeats, got %d", received.Load())
	}
}
