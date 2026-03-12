package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/cogitatorai/cogitator/server/internal/ollama"
)

func (r *Router) handleOllamaStatus(w http.ResponseWriter, req *http.Request) {
	running := r.ollama.Status(req.Context())
	writeJSON(w, http.StatusOK, map[string]bool{"running": running})
}

func (r *Router) handleListOllamaModels(w http.ResponseWriter, req *http.Request) {
	models, err := r.ollama.ListModels(req.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to list models: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func (r *Router) handlePullOllamaModel(w http.ResponseWriter, req *http.Request) {
	if !requireAdmin(w, req) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	slog.Info("ollama: pull started", "model", body.Name)

	progress := make(chan ollama.PullProgress, 16)
	errCh := make(chan error, 1)

	go func() {
		errCh <- r.ollama.PullModel(req.Context(), body.Name, progress)
	}()

	for p := range progress {
		data, _ := json.Marshal(p)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	if err := <-errCh; err != nil {
		slog.Error("ollama: pull failed", "model", body.Name, "error", err)
		errEvt, _ := json.Marshal(map[string]string{"status": "error", "error": err.Error()})
		fmt.Fprintf(w, "data: %s\n\n", errEvt)
		flusher.Flush()
	} else {
		slog.Info("ollama: pull complete", "model", body.Name)
	}
}

func (r *Router) handleDeleteOllamaModel(w http.ResponseWriter, req *http.Request) {
	if !requireAdmin(w, req) {
		return
	}
	name := req.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "model name is required")
		return
	}
	if err := r.ollama.DeleteModel(req.Context(), name); err != nil {
		writeError(w, http.StatusBadGateway, "failed to delete model: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
