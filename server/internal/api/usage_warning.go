package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type usageWarningPushRequest struct {
	Level      string  `json:"level"`
	UsagePct   float64 `json:"usage_pct"`
	PeriodEnd  string  `json:"period_end"`
	UpgradeURL string  `json:"upgrade_url"`
}

// handleUsageWarningPush receives usage warning updates from the orchestrator.
func (rt *Router) handleUsageWarningPush(w http.ResponseWriter, r *http.Request) {
	var req usageWarningPushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	rt.usageWarningMu.Lock()
	rt.usageWarningLevel = req.Level
	rt.usageWarningPct = req.UsagePct
	rt.usageWarningPeriodEnd = req.PeriodEnd
	rt.usageWarningUpgradeURL = req.UpgradeURL
	rt.usageWarningMu.Unlock()

	slog.Info("usage warning updated", "level", req.Level, "usage_pct", req.UsagePct)
	w.WriteHeader(http.StatusOK)
}

// handleUsageWarningGet returns the current usage warning for the dashboard.
func (rt *Router) handleUsageWarningGet(w http.ResponseWriter, r *http.Request) {
	rt.usageWarningMu.RLock()
	resp := map[string]any{
		"level":       rt.usageWarningLevel,
		"usage_pct":   rt.usageWarningPct,
		"period_end":  rt.usageWarningPeriodEnd,
		"upgrade_url": rt.usageWarningUpgradeURL,
	}
	rt.usageWarningMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
