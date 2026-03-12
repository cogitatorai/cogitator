package api

import (
	"encoding/json"
	"net/http"
)

func (r *Router) handleGetVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, r.updater.Status())
}

func (r *Router) handleCheckVersion(w http.ResponseWriter, _ *http.Request) {
	r.updater.CheckNow()
	writeJSON(w, http.StatusOK, r.updater.Status())
}

func (r *Router) handleDownloadUpdate(w http.ResponseWriter, req *http.Request) {
	if !requireAdmin(w, req) {
		return
	}
	go r.updater.Download()
	writeJSON(w, http.StatusAccepted, r.updater.Status())
}

func (r *Router) handleRestartUpdate(w http.ResponseWriter, req *http.Request) {
	if !requireAdmin(w, req) {
		return
	}
	shutdownFn := func() {
		if r.shutdownFn != nil {
			r.shutdownFn()
		}
	}
	if err := r.updater.Restart(shutdownFn); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "restarting"})
}

func (r *Router) handleSkipVersion(w http.ResponseWriter, req *http.Request) {
	if !requireAdmin(w, req) {
		return
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cfg := r.configStore.Get()
	cfg.Update.SkippedVersion = body.Version
	if err := r.configStore.Save(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	r.updater.SetSkippedVersion(body.Version)
	writeJSON(w, http.StatusOK, r.updater.Status())
}
