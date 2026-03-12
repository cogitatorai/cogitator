package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/agent"
	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/config"
	"github.com/cogitatorai/cogitator/server/internal/database"
	"github.com/cogitatorai/cogitator/server/internal/provider"
	"github.com/cogitatorai/cogitator/server/internal/session"
)

func setupSettingsRouter(t *testing.T) (*Router, string) {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	profilePath := filepath.Join(dir, "profile.md")
	os.WriteFile(profilePath, []byte(""), 0o644)

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := config.Default()
	store := config.NewStore(cfg, cfgPath, nil)

	eventBus := bus.New()
	t.Cleanup(func() { eventBus.Close() })

	a := agent.New(agent.Config{
		Sessions:       session.NewStore(db),
		ContextBuilder: agent.NewContextBuilder(profilePath),
		EventBus:       eventBus,
		Model:          "test",
	})

	factory := func(name, apiKey string) (provider.Provider, error) {
		return provider.NewMock(provider.Response{Content: "configured via " + name}), nil
	}

	router := NewRouter(RouterConfig{
		Agent:           a,
		ConfigStore:     store,
		ProviderFactory: factory,
	})

	return router, cfgPath
}

func TestGetSettings(t *testing.T) {
	router, _ := setupSettingsRouter(t)

	req := httptest.NewRequest("GET", "/api/settings", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp settingsResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Models.Standard.Provider != "" {
		t.Errorf("expected empty provider, got %q", resp.Models.Standard.Provider)
	}
	if len(resp.Providers) != 0 {
		t.Errorf("expected no providers, got %d", len(resp.Providers))
	}
}

func TestUpdateSettings(t *testing.T) {
	router, cfgPath := setupSettingsRouter(t)

	body := `{
		"models": {"standard": {"provider": "openai", "model": "gpt-4o"}},
		"providers": {"openai": {"api_key": "sk-test123"}}
	}`
	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp settingsResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Models.Standard.Provider != "openai" {
		t.Errorf("expected 'openai', got %q", resp.Models.Standard.Provider)
	}
	if resp.Models.Standard.Model != "gpt-4o" {
		t.Errorf("expected 'gpt-4o', got %q", resp.Models.Standard.Model)
	}
	if !resp.Providers["openai"].APIKeySet {
		t.Error("expected openai api_key_set true")
	}

	// Config should be persisted to disk
	if _, err := os.Stat(cfgPath); err != nil {
		t.Errorf("config file not written: %v", err)
	}
}

func TestUpdateSettingsSharedProvider(t *testing.T) {
	router, _ := setupSettingsRouter(t)

	// Set both tiers to the same provider, one API key
	body := `{
		"models": {
			"standard": {"provider": "openai", "model": "gpt-4o"},
			"cheap": {"provider": "openai", "model": "gpt-4o-mini"}
		},
		"providers": {"openai": {"api_key": "sk-shared"}}
	}`
	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp settingsResponse
	json.NewDecoder(w.Body).Decode(&resp)

	// Both tiers reference the same provider
	if resp.Models.Standard.Provider != "openai" {
		t.Errorf("standard: expected 'openai', got %q", resp.Models.Standard.Provider)
	}
	if resp.Models.Cheap.Provider != "openai" {
		t.Errorf("cheap: expected 'openai', got %q", resp.Models.Cheap.Provider)
	}
	// Only one provider entry with a key
	if len(resp.Providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(resp.Providers))
	}
	if !resp.Providers["openai"].APIKeySet {
		t.Error("expected openai api_key_set true")
	}
}

func TestUpdateSettingsHotSwapsProvider(t *testing.T) {
	router, _ := setupSettingsRouter(t)

	if router.agent.ProviderConfigured() {
		t.Fatal("expected no provider initially")
	}

	body := `{
		"models": {"standard": {"provider": "openai", "model": "gpt-4o"}},
		"providers": {"openai": {"api_key": "sk-test"}}
	}`
	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if !router.agent.ProviderConfigured() {
		t.Error("expected provider to be configured after settings update")
	}
}

func TestUpdateSettingsInvalidJSON(t *testing.T) {
	router, _ := setupSettingsRouter(t)

	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestUpdateSettingsPublicURL(t *testing.T) {
	router, cfgPath := setupSettingsRouter(t)

	body := `{"server": {"public_url": "https://cogitator.example.com/"}}`
	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp settingsResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Server.PublicURL != "https://cogitator.example.com" {
		t.Errorf("expected trimmed URL, got %q", resp.Server.PublicURL)
	}

	// Verify persisted to disk
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !bytes.Contains(data, []byte("public_url")) {
		t.Error("public_url not found in config file")
	}
}

func TestUpdateSettingsPublicURL_Invalid(t *testing.T) {
	router, _ := setupSettingsRouter(t)

	body := `{"server": {"public_url": "not-a-url"}}`
	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestStatusIncludesProviderConfigured(t *testing.T) {
	router, _ := setupSettingsRouter(t)

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var status map[string]any
	json.NewDecoder(w.Body).Decode(&status)

	if status["provider_configured"] != false {
		t.Errorf("expected provider_configured false, got %v", status["provider_configured"])
	}
}
