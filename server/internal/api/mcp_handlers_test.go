package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/mcp"
	"github.com/cogitatorai/cogitator/server/internal/secretstore"
)

// setupMCPRouter creates a Router wired to a real mcp.Manager backed by a
// temp directory, suitable for testing the MCP API handlers.
func setupMCPRouter(t *testing.T) *Router {
	t.Helper()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp.json")
	store := secretstore.NewFileStore(dir)

	eb := bus.New()
	t.Cleanup(func() { eb.Close() })

	mgr := mcp.NewManager(cfgPath, store, eb, nil)
	if err := mgr.LoadConfig(); err != nil {
		t.Fatalf("load config: %v", err)
	}

	return NewRouter(RouterConfig{MCP: mgr})
}

func TestAddMCPServer_WithInstructions(t *testing.T) {
	router := setupMCPRouter(t)

	payload := `{"name":"test-srv","command":"echo","args":["hello"],"instructions":"Search the web"}`
	req := httptest.NewRequest("POST", "/api/mcp/servers", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Verify instructions are persisted by listing servers.
	req = httptest.NewRequest("GET", "/api/mcp/servers", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var listResp struct {
		Servers []mcp.ServerStatus `json:"servers"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	found := false
	for _, s := range listResp.Servers {
		if s.Name == "test-srv" {
			found = true
			if s.Instructions != "Search the web" {
				t.Errorf("expected instructions %q, got %q", "Search the web", s.Instructions)
			}
		}
	}
	if !found {
		t.Fatal("server test-srv not found in list")
	}
}

func TestAddMCPServer_WithoutInstructions(t *testing.T) {
	router := setupMCPRouter(t)

	payload := `{"name":"plain","command":"echo"}`
	req := httptest.NewRequest("POST", "/api/mcp/servers", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateMCPServer_SetInstructions(t *testing.T) {
	router := setupMCPRouter(t)

	// Add a server first.
	payload := `{"name":"upd-srv","command":"echo"}`
	req := httptest.NewRequest("POST", "/api/mcp/servers", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("add: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Update instructions via PATCH.
	patch := `{"instructions":"New instructions"}`
	req = httptest.NewRequest("PATCH", "/api/mcp/servers/upd-srv", bytes.NewBufferString(patch))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("patch: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "updated" {
		t.Errorf("expected status 'updated', got %q", resp["status"])
	}

	// Verify via GET list.
	req = httptest.NewRequest("GET", "/api/mcp/servers", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var listResp struct {
		Servers []mcp.ServerStatus `json:"servers"`
	}
	json.NewDecoder(w.Body).Decode(&listResp)

	for _, s := range listResp.Servers {
		if s.Name == "upd-srv" && s.Instructions != "New instructions" {
			t.Errorf("expected instructions %q, got %q", "New instructions", s.Instructions)
		}
	}
}

func TestUpdateMCPServer_ClearInstructions(t *testing.T) {
	router := setupMCPRouter(t)

	// Add with instructions.
	payload := `{"name":"clr-srv","command":"echo","instructions":"initial"}`
	req := httptest.NewRequest("POST", "/api/mcp/servers", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("add: expected 201, got %d", w.Code)
	}

	// Clear instructions by sending empty string.
	patch := `{"instructions":""}`
	req = httptest.NewRequest("PATCH", "/api/mcp/servers/clr-srv", bytes.NewBufferString(patch))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("patch: expected 200, got %d", w.Code)
	}

	// Verify cleared.
	req = httptest.NewRequest("GET", "/api/mcp/servers", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var listResp struct {
		Servers []mcp.ServerStatus `json:"servers"`
	}
	json.NewDecoder(w.Body).Decode(&listResp)

	for _, s := range listResp.Servers {
		if s.Name == "clr-srv" && s.Instructions != "" {
			t.Errorf("expected empty instructions, got %q", s.Instructions)
		}
	}
}

func TestUpdateMCPServer_UnknownServer(t *testing.T) {
	router := setupMCPRouter(t)

	patch := `{"instructions":"anything"}`
	req := httptest.NewRequest("PATCH", "/api/mcp/servers/nonexistent", bytes.NewBufferString(patch))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateMCPServer_InvalidJSON(t *testing.T) {
	router := setupMCPRouter(t)

	req := httptest.NewRequest("PATCH", "/api/mcp/servers/any", bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateMCPServer_PersistsToFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp.json")
	store := secretstore.NewFileStore(dir)

	eb := bus.New()
	t.Cleanup(func() { eb.Close() })

	mgr := mcp.NewManager(cfgPath, store, eb, nil)
	if err := mgr.LoadConfig(); err != nil {
		t.Fatalf("load config: %v", err)
	}

	router := NewRouter(RouterConfig{MCP: mgr})

	// Add a server.
	payload := `{"name":"persist","command":"echo"}`
	req := httptest.NewRequest("POST", "/api/mcp/servers", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("add: expected 201, got %d", w.Code)
	}

	// PATCH instructions.
	patch := `{"instructions":"persisted value"}`
	req = httptest.NewRequest("PATCH", "/api/mcp/servers/persist", bytes.NewBufferString(patch))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("patch: expected 200, got %d", w.Code)
	}

	// Read raw config file and verify instructions persisted.
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	servers, ok := raw["mcpServers"]
	if !ok {
		t.Fatal("missing mcpServers key in config")
	}

	serverRaw, ok := servers["persist"]
	if !ok {
		t.Fatal("missing persist server in config")
	}

	var cfg mcp.ServerConfig
	if err := json.Unmarshal(serverRaw, &cfg); err != nil {
		t.Fatalf("unmarshal server config: %v", err)
	}

	if cfg.Instructions != "persisted value" {
		t.Errorf("expected persisted instructions %q, got %q", "persisted value", cfg.Instructions)
	}
}
