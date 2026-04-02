package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/cogitatorai/cogitator/server/internal/frontend"
)

// allowedFrontendURLPrefixes restricts where the update-frontend endpoint
// can download tarballs from. This prevents a compromised internal secret
// from being used to inject a malicious dashboard.
var allowedFrontendURLPrefixes = []string{
	"https://assets.cogitator.cloud/",
}

func (r *Router) handleMetrics(w http.ResponseWriter, req *http.Request) {
	snap := r.metricsRing.Snapshot()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(snap)
}

func (r *Router) handleDrain(w http.ResponseWriter, req *http.Request) {
	r.drainManager.Start()

	activeTasks := 0
	if r.tasks != nil {
		activeTasks, _ = r.tasks.CountActiveRuns()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"drained":      activeTasks == 0,
		"active_tasks": activeTasks,
	})
}

func (r *Router) handleUpdateFrontend(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Version string `json:"version"`
		URL     string `json:"url"`
		SHA256  string `json:"sha256"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if body.URL == "" || body.SHA256 == "" {
		http.Error(w, "url and sha256 are required", http.StatusBadRequest)
		return
	}

	allowed := false
	for _, prefix := range allowedFrontendURLPrefixes {
		if strings.HasPrefix(body.URL, prefix) {
			allowed = true
			break
		}
	}
	if !allowed {
		http.Error(w, "url not allowed", http.StatusForbidden)
		return
	}

	if err := frontend.DownloadAndSwap(r.dashboardDir, body.URL, body.SHA256); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"version": body.Version,
		"updated": true,
	})
}
