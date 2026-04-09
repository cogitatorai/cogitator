package api

import (
	"io"
	"net/http"
	"time"
)

var meteringClient = &http.Client{Timeout: 10 * time.Second}

// handleMeteringProxy fetches the tenant's metering status from the orchestrator.
// Only available in SaaS mode.
func (rt *Router) handleMeteringProxy(w http.ResponseWriter, r *http.Request) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet,
		rt.orchestratorURL+"/api/internal/metering", nil)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	req.Header.Set("X-Tenant-ID", rt.tenantID)
	req.Header.Set("X-Internal-Secret", rt.internalSecret)

	resp, err := meteringClient.Do(req)
	if err != nil {
		http.Error(w, `{"error":"orchestrator unreachable"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body) //nolint:errcheck
}
