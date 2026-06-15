package api

import "net/http"

// handleRetrievalTraces returns the recent retrieval traces (newest first).
// Admin-gated at the router. The ring is empty unless COGITATOR_RETRIEVAL_TRACE
// is enabled; the response is still a valid empty list in that case. Optional
// ?session= and ?user= filters narrow the result.
func (r *Router) handleRetrievalTraces(w http.ResponseWriter, req *http.Request) {
	if r.retrievalTraces == nil {
		writeJSON(w, http.StatusOK, map[string]any{"traces": []any{}})
		return
	}
	sessionFilter := req.URL.Query().Get("session")
	userFilter := req.URL.Query().Get("user")

	all := r.retrievalTraces.Snapshot()
	traces := all[:0:0]
	for _, t := range all {
		if sessionFilter != "" && t.SessionKey != sessionFilter {
			continue
		}
		if userFilter != "" && t.UserID != userFilter {
			continue
		}
		traces = append(traces, t)
	}
	writeJSON(w, http.StatusOK, map[string]any{"traces": traces})
}
