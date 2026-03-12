package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/mcp"
)

func (r *Router) handleListMCPServers(w http.ResponseWriter, req *http.Request) {
	servers := r.mcp.Servers()
	writeJSON(w, http.StatusOK, map[string]any{"servers": servers})
}

func (r *Router) handleAddMCPServer(w http.ResponseWriter, req *http.Request) {
	if !requireAdmin(w, req) {
		return
	}
	var body struct {
		Name         string            `json:"name"`
		Command      string            `json:"command"`
		Args         []string          `json:"args"`
		Env          map[string]string `json:"env"`
		URL          string            `json:"url"`
		Transport    string            `json:"transport"`
		Headers      map[string]string `json:"headers"`
		Instructions string            `json:"instructions"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if body.Command == "" && body.URL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "command or url is required"})
		return
	}
	if err := r.mcp.AddServer(body.Name, mcp.ServerConfig{
		Command:      body.Command,
		Args:         body.Args,
		Env:          body.Env,
		URL:          body.URL,
		Transport:    body.Transport,
		Headers:      body.Headers,
		Instructions: body.Instructions,
	}); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}

func (r *Router) handleUpdateMCPServer(w http.ResponseWriter, req *http.Request) {
	if !requireAdmin(w, req) {
		return
	}
	name := req.PathValue("name")
	var body struct {
		Instructions *string `json:"instructions"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	if err := r.mcp.UpdateServer(name, func(cfg *mcp.ServerConfig) {
		if body.Instructions != nil {
			cfg.Instructions = *body.Instructions
		}
	}); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (r *Router) handleUpdateMCPSecrets(w http.ResponseWriter, req *http.Request) {
	if !requireAdmin(w, req) {
		return
	}
	name := req.PathValue("name")

	var body struct {
		Headers map[string]string `json:"headers"`
		OAuth   *mcp.OAuthSecrets `json:"oauth"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	if err := r.mcp.SaveServerSecrets(name, &mcp.ServerSecrets{
		Headers: body.Headers,
		OAuth:   body.OAuth,
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (r *Router) handleRemoveMCPServer(w http.ResponseWriter, req *http.Request) {
	if !requireAdmin(w, req) {
		return
	}
	name := req.PathValue("name")
	if err := r.mcp.RemoveServer(name); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (r *Router) handleStartMCPServer(w http.ResponseWriter, req *http.Request) {
	if !requireAdmin(w, req) {
		return
	}
	name := req.PathValue("name")
	if err := r.mcp.StartServer(req.Context(), name); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "running"})
}

func (r *Router) handleStopMCPServer(w http.ResponseWriter, req *http.Request) {
	if !requireAdmin(w, req) {
		return
	}
	name := req.PathValue("name")
	if err := r.mcp.StopServer(name); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (r *Router) handleListMCPTools(w http.ResponseWriter, req *http.Request) {
	name := req.PathValue("name")
	toolList, err := r.mcp.Tools(req.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": toolList})
}

func (r *Router) handleTestMCPTool(w http.ResponseWriter, req *http.Request) {
	if !requireAdmin(w, req) {
		return
	}
	serverName := req.PathValue("name")
	toolName := req.PathValue("tool")

	var body struct {
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	start := time.Now()
	result, err := r.mcp.CallTool(req.Context(), serverName, toolName, body.Arguments)
	duration := time.Since(start).Milliseconds()

	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"result":      "",
			"duration_ms": duration,
			"error":       err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"result":      result,
		"duration_ms": duration,
		"error":       nil,
	})
}
