package api

import (
	"context"
	"net/http"
	"runtime"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/mcp"
)

// handleReady reports component readiness. DB failure makes the instance
// unready (503); a missing provider or down MCP server is reported but does
// not flip readiness, because the instance can still serve auth, setup, and
// the dashboard. /api/health remains the trivially cheap liveness probe.
func (r *Router) handleReady(w http.ResponseWriter, req *http.Request) {
	checks := map[string]bool{}
	ready := true

	if r.db != nil {
		ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
		defer cancel()
		var one int
		dbOK := r.db.Reader().QueryRowContext(ctx, "SELECT 1").Scan(&one) == nil
		checks["db"] = dbOK
		if !dbOK {
			ready = false
		}
	}

	providerConfigured := false
	if r.agent != nil {
		providerConfigured = r.agent.ProviderConfigured()
	}
	checks["provider"] = providerConfigured

	if r.mcp != nil {
		allRunning := true
		for _, s := range r.mcp.Servers() {
			if s.Status != mcp.StatusRunning {
				allRunning = false
				break
			}
		}
		checks["mcp"] = allRunning
	}

	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{"ready": ready, "checks": checks})
}

var startTime = time.Now()

func (r *Router) handleSystemStatus(w http.ResponseWriter, req *http.Request) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	providerConfigured := false
	if r.agent != nil {
		providerConfigured = r.agent.ProviderConfigured()
	}

	status := map[string]any{
		"uptime_seconds":      int(time.Since(startTime).Seconds()),
		"go_version":          runtime.Version(),
		"goroutines":          runtime.NumGoroutine(),
		"provider_configured": providerConfigured,
		"desktop_mode":        r.dashboardFS != nil,
		"saas":                r.isSaaS,
		"memory": map[string]any{
			"alloc_mb":       memStats.Alloc / 1024 / 1024,
			"total_alloc_mb": memStats.TotalAlloc / 1024 / 1024,
			"sys_mb":         memStats.Sys / 1024 / 1024,
			"num_gc":         memStats.NumGC,
		},
	}

	// Add component counts if available
	components := map[string]any{}
	uid := userIDFromRequest(req)
	if r.sessions != nil {
		if sessions, err := r.sessions.List(uid); err == nil {
			components["sessions"] = len(sessions)
		}
	}
	if r.memory != nil {
		if stats, err := r.memory.Stats(); err == nil {
			components["memory_nodes"] = stats["total_nodes"]
		}
	}
	if r.tasks != nil {
		if tasks, err := r.tasks.ListTasks(uid); err == nil {
			components["tasks"] = len(tasks)
		}
	}
	if r.tools != nil {
		components["tools"] = len(r.tools.List())
	}
	if r.skills != nil {
		if skills, err := r.skills.List(); err == nil {
			components["skills"] = len(skills)
		}
	}
	status["components"] = components

	writeJSON(w, http.StatusOK, status)
}
