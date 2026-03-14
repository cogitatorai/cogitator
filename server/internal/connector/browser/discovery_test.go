package browser

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(VersionInfo{
			Browser:              "Chrome/125.0.0.0",
			WebSocketDebuggerURL: "ws://127.0.0.1:9222/devtools/browser/abc",
		})
	}))
	defer srv.Close()

	info, err := GetVersion(srv.URL)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if info.Browser != "Chrome/125.0.0.0" {
		t.Errorf("got browser %q", info.Browser)
	}
	if info.WebSocketDebuggerURL == "" {
		t.Error("missing ws URL")
	}
}

func TestGetVersionUnreachable(t *testing.T) {
	_, err := GetVersion("http://127.0.0.1:19999")
	if err == nil {
		t.Fatal("expected error for unreachable port")
	}
}

func TestListTargets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]TargetInfo{
			{ID: "ABC123", Type: "page", Title: "Example", URL: "https://example.com"},
			{ID: "DEF456", Type: "page", Title: "Chrome Settings", URL: "chrome://settings"},
			{ID: "GHI789", Type: "background_page", Title: "Extension", URL: "chrome-extension://abc"},
		})
	}))
	defer srv.Close()

	targets, err := ListTargets(srv.URL)
	if err != nil {
		t.Fatalf("ListTargets: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 page target (excluding chrome:// and non-page), got %d", len(targets))
	}
	if targets[0].ID != "ABC123" {
		t.Errorf("expected ABC123, got %s", targets[0].ID)
	}
}
