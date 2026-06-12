package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
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
