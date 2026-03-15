package api

import (
	"net/http"
	"runtime"
	"time"
)

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
