package api

import (
	"net/http"
)

func (r *Router) handleListTools(w http.ResponseWriter, _ *http.Request) {
	tools := r.tools.List()
	writeJSON(w, http.StatusOK, tools)
}

func (r *Router) handleGetTool(w http.ResponseWriter, req *http.Request) {
	name := req.PathValue("name")
	tool, ok := r.tools.Get(name)
	if !ok {
		writeError(w, http.StatusNotFound, "tool not found")
		return
	}
	writeJSON(w, http.StatusOK, tool)
}

func (r *Router) handleDeleteTool(w http.ResponseWriter, req *http.Request) {
	if !requireAdmin(w, req) {
		return
	}
	name := req.PathValue("name")
	if !r.tools.Delete(name) {
		writeError(w, http.StatusForbidden, "cannot delete built-in tool")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
