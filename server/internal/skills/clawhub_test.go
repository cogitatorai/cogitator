package skills

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// makeSkillZip creates a ZIP archive containing SKILL.md and _meta.json.
func makeSkillZip(t *testing.T, skillContent string, meta SkillZipMeta) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	fw, err := zw.Create("SKILL.md")
	if err != nil {
		t.Fatalf("zip create SKILL.md: %v", err)
	}
	fw.Write([]byte(skillContent))

	fw, err = zw.Create("_meta.json")
	if err != nil {
		t.Fatalf("zip create _meta.json: %v", err)
	}
	metaBytes, _ := json.Marshal(meta)
	fw.Write(metaBytes)

	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func TestClawHubSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("q") == "" {
			http.Error(w, "missing query", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SearchResult{
			Results: []SkillMeta{
				{Slug: "weather", DisplayName: "Weather", Summary: "Get weather", Version: "1.0.0", Score: 0.95},
				{Slug: "calendar", DisplayName: "Calendar", Summary: "Calendar ops", Version: "2.0.0"},
			},
		})
	}))
	defer srv.Close()

	client := NewClawHub(srv.URL, nil)
	got, err := client.Search(context.Background(), "weather")
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(got.Results) != 2 {
		t.Fatalf("len(Results) = %d, want 2", len(got.Results))
	}
	if got.Results[0].Slug != "weather" {
		t.Errorf("Results[0].Slug = %q, want %q", got.Results[0].Slug, "weather")
	}
	if got.Results[0].Version != "1.0.0" {
		t.Errorf("Results[0].Version = %q, want %q", got.Results[0].Version, "1.0.0")
	}
	if got.Results[1].DisplayName != "Calendar" {
		t.Errorf("Results[1].DisplayName = %q, want %q", got.Results[1].DisplayName, "Calendar")
	}
}

func TestClawHubDownloadSkill(t *testing.T) {
	wantContent := "# Weather\n\nUse curl wttr.in.\n"
	wantMeta := SkillZipMeta{
		OwnerID:     "owner1",
		Slug:        "weather",
		Version:     "1.0.0",
		PublishedAt: 1767545394459,
	}

	zipData := makeSkillZip(t, wantContent, wantMeta)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/download" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("slug") == "" {
			http.Error(w, "missing slug", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Write(zipData)
	}))
	defer srv.Close()

	client := NewClawHub(srv.URL, nil)
	content, meta, err := client.DownloadSkill(context.Background(), "weather")
	if err != nil {
		t.Fatalf("DownloadSkill() error: %v", err)
	}
	if string(content) != wantContent {
		t.Errorf("content = %q, want %q", string(content), wantContent)
	}
	if meta.Slug != wantMeta.Slug {
		t.Errorf("meta.Slug = %q, want %q", meta.Slug, wantMeta.Slug)
	}
	if meta.Version != wantMeta.Version {
		t.Errorf("meta.Version = %q, want %q", meta.Version, wantMeta.Version)
	}
	if meta.OwnerID != wantMeta.OwnerID {
		t.Errorf("meta.OwnerID = %q, want %q", meta.OwnerID, wantMeta.OwnerID)
	}
}

func TestClawHubSearchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClawHub(srv.URL, nil)
	_, err := client.Search(context.Background(), "anything")
	if err == nil {
		t.Fatal("Search() expected error for 500 response, got nil")
	}
}

func TestClawHubDownloadSkillError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewClawHub(srv.URL, nil)
	_, _, err := client.DownloadSkill(context.Background(), "missing")
	if err == nil {
		t.Fatal("DownloadSkill() expected error for 404 response, got nil")
	}
}

func TestParseSlug(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"weather", "weather"},
		{"web-search", "web-search"},
		{"https://clawhub.ai/skills/weather", "weather"},
		{"https://clawhub.ai/skills/france-cinemas", "france-cinemas"},
		{"https://clawhub.ai/skills/weather/", "weather"},
		{"http://clawhub.ai/skills/weather", "weather"},
		{"https://clawhub.ai/other/weather", "https://clawhub.ai/other/weather"},
		{"https://clawhub.ai/skills", "https://clawhub.ai/skills"},
		{"https://example.com/skills/weather", "weather"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := ParseSlug(tt.input); got != tt.want {
			t.Errorf("ParseSlug(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestClawHubDefaultBaseURL(t *testing.T) {
	ch := NewClawHub("", nil)
	if ch.baseURL != "https://clawhub.ai" {
		t.Errorf("baseURL = %q, want %q", ch.baseURL, "https://clawhub.ai")
	}
}
