package heartbeat

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNotifyWakeTime(t *testing.T) {
	wakeAt := time.Date(2026, 3, 11, 14, 30, 0, 0, time.UTC)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/internal/schedule-wake" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing Content-Type header")
		}
		if r.Header.Get("X-Internal-Secret") != "test-secret" {
			t.Errorf("wrong secret header: %s", r.Header.Get("X-Internal-Secret"))
		}

		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decoding payload: %v", err)
		}
		if payload["tenant_id"] != "tenant-42" {
			t.Errorf("unexpected tenant_id: %s", payload["tenant_id"])
		}
		if payload["wake_at"] != "2026-03-11T14:30:00Z" {
			t.Errorf("unexpected wake_at: %s", payload["wake_at"])
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	err := NotifyWakeTime(server.URL, "tenant-42", "test-secret", wakeAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNotifyWakeTimeErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	err := NotifyWakeTime(server.URL, "tenant-42", "secret", time.Now())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}
