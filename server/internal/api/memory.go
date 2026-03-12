package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/memory"
)

func (r *Router) handleMemoryStats(w http.ResponseWriter, _ *http.Request) {
	stats, err := r.memory.Stats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get stats")
		return
	}

	enriching := 0
	if r.enrichStatus != nil && r.enrichStatus.IsActive() {
		enriching = 1
	}
	stats["enriching"] = enriching

	writeJSON(w, http.StatusOK, stats)
}

func (r *Router) handleListMemoryNodes(w http.ResponseWriter, req *http.Request) {
	nodeType := memory.NodeType(req.URL.Query().Get("type"))

	limit := 100
	if l := req.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	offset := 0
	if o := req.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	nodes, err := r.memory.ListNodes(userIDFromRequest(req), nodeType, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list nodes")
		return
	}
	if nodes == nil {
		nodes = []memory.Node{}
	}
	writeJSON(w, http.StatusOK, nodes)
}

func (r *Router) handleCreateMemoryNode(w http.ResponseWriter, req *http.Request) {
	var node memory.Node
	if err := json.NewDecoder(req.Body).Decode(&node); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if node.Type == "" || node.Title == "" {
		writeError(w, http.StatusBadRequest, "type and title are required")
		return
	}

	// Manually created nodes are shared by default. If the caller explicitly
	// wants a private node, they set user_id in the JSON body. Otherwise we
	// leave it nil (shared).

	uid := userIDFromRequest(req)

	// Non-admin users cannot set user_id or subject_id to another user.
	if hasAuth(req) && !isAdmin(req) {
		if node.UserID != nil && *node.UserID != uid {
			writeError(w, http.StatusForbidden, "cannot create memories owned by another user")
			return
		}
		if node.SubjectID != nil && *node.SubjectID != uid {
			writeError(w, http.StatusForbidden, "cannot create memories about another user")
			return
		}
	}

	id, err := r.memory.CreateNode(&node)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create node")
		return
	}

	if r.eventBus != nil {
		r.eventBus.Publish(bus.Event{
			Type:    bus.EnrichmentQueued,
			Payload: map[string]any{"node_id": id},
		})
	}

	created, _ := r.memory.GetNode(id)
	writeJSON(w, http.StatusCreated, created)
}

func (r *Router) handleGetMemoryNode(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	node, err := r.memory.GetNode(id)
	if err == memory.ErrNotFound {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get node")
		return
	}
	if !canAccessNode(req, node.UserID) {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	writeJSON(w, http.StatusOK, node)
}

func (r *Router) handleDeleteMemoryNode(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	node, err := r.memory.GetNode(id)
	if err == memory.ErrNotFound {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get node")
		return
	}
	// Private nodes: only the owner or admin can delete.
	// Shared nodes: only admin or moderator can delete.
	if node.UserID != nil {
		if !canAccessNode(req, node.UserID) {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}
	} else {
		if !isAdminOrMod(req) && hasAuth(req) {
			writeError(w, http.StatusForbidden, "only admins and moderators can delete shared memories")
			return
		}
	}
	if err := r.memory.DeleteNode(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete node")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (r *Router) handleGetMemoryEdges(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	// Verify the source node is visible to the caller.
	node, err := r.memory.GetNode(id)
	if err != nil || !canAccessNode(req, node.UserID) {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	uid := userIDFromRequest(req)
	from, _ := r.memory.GetEdgesFrom(id, uid)
	to, _ := r.memory.GetEdgesTo(id, uid)

	if from == nil {
		from = []memory.Edge{}
	}
	if to == nil {
		to = []memory.Edge{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"outgoing": from,
		"incoming": to,
	})
}

func (r *Router) handleGetConnectedNodes(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	// Verify the source node is visible to the caller.
	node, err := r.memory.GetNode(id)
	if err != nil || !canAccessNode(req, node.UserID) {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	uid := userIDFromRequest(req)
	connected, err := r.memory.GetConnectedNodesByUser(id, uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get connected nodes")
		return
	}
	if connected == nil {
		connected = []memory.NodeSummary{}
	}
	writeJSON(w, http.StatusOK, connected)
}

func (r *Router) handleMemoryGraph(w http.ResponseWriter, req *http.Request) {
	uid := userIDFromRequest(req)
	nodes, err := r.memory.GetNodeSummaries(uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list nodes")
		return
	}
	edges, err := r.memory.GetVisibleEdges(uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list edges")
		return
	}
	if nodes == nil {
		nodes = []memory.NodeSummary{}
	}
	if edges == nil {
		edges = []memory.Edge{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nodes": nodes,
		"edges": edges,
	})
}

func (r *Router) handlePinNode(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	var body struct {
		Pinned bool `json:"pinned"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	node, err := r.memory.GetNode(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	if !canAccessNode(req, node.UserID) {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	node.Pinned = body.Pinned
	if err := r.memory.UpdateNode(node); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update node")
		return
	}
	writeJSON(w, http.StatusOK, node)
}

func (r *Router) handleToggleNodePrivacy(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	var body struct {
		Private bool `json:"private"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	node, err := r.memory.GetNode(id)
	if err == memory.ErrNotFound {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get node")
		return
	}

	uid := userIDFromRequest(req)

	// Authorization: owner or admin can toggle.
	// Shared nodes: any authenticated user can claim (make private).
	if node.UserID != nil && !canAccessNode(req, node.UserID) {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}

	var newUserID *string
	if body.Private {
		if uid == "" {
			writeError(w, http.StatusBadRequest, "cannot make private without authentication")
			return
		}
		newUserID = &uid
	}

	if err := r.memory.SetNodePrivacy(id, newUserID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to toggle privacy")
		return
	}

	// Invalidate retriever cache.
	if r.retriever != nil {
		r.retriever.InvalidateCache()
	}

	updated, _ := r.memory.GetNode(id)
	writeJSON(w, http.StatusOK, updated)
}

func (r *Router) handleTriggerEnrichment(w http.ResponseWriter, _ *http.Request) {
	if r.eventBus == nil {
		writeError(w, http.StatusServiceUnavailable, "event bus not available")
		return
	}
	pending, _ := r.memory.GetPendingEnrichment(1)
	slog.Info("enrichment triggered via API", "pending_nodes", len(pending))
	r.eventBus.Publish(bus.Event{Type: bus.EnrichmentQueued})
	writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
}
