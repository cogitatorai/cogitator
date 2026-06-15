package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/metrics"
)

func TestReadyEndpoint(t *testing.T) {
	router := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/ready", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var body struct {
		Ready  bool            `json:"ready"`
		Checks map[string]bool `json:"checks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !body.Ready {
		t.Errorf("ready = false; body: %s", w.Body.String())
	}
	if dbOK, ok := body.Checks["db"]; !ok || !dbOK {
		t.Errorf("checks.db = %v, %v; want true", dbOK, ok)
	}
	if _, ok := body.Checks["provider"]; !ok {
		t.Errorf("checks.provider missing; body: %s", w.Body.String())
	}
}

func TestStatusIncludesHealth(t *testing.T) {
	router := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	health, ok := body["health"].(map[string]any)
	if !ok {
		t.Fatalf("health section missing: %v", body)
	}
	if health["db"] != true {
		t.Errorf("health.db = %v, want true", health["db"])
	}
	if _, ok := health["provider"]; !ok {
		t.Errorf("health.provider missing: %v", health)
	}
}

func TestStatusIncludesRetrievalMetrics(t *testing.T) {
	router := setupTestRouter(t)
	router.retrievalStats = metricsNewRetrievalStatsForTest()

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := body["retrieval"]; !ok {
		t.Errorf("retrieval section missing: %v", body)
	}
}

func metricsNewRetrievalStatsForTest() *metrics.RetrievalStats {
	s := metrics.NewRetrievalStats(10)
	s.Record(0.8, 2, 0.5)
	return s
}
