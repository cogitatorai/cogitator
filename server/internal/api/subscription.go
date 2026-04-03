package api

import (
	"encoding/json"
	"log"
	"net/http"
)

// requireActiveSubscription blocks write operations when subscription is in
// grace_period or expired. Only applies in SaaS mode. Self-hosted instances
// bypass this check.
func (rt *Router) requireActiveSubscription(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !rt.isSaaS {
			next(w, r)
			return
		}

		var status string
		err := rt.db.QueryRow(
			`SELECT status FROM subscription_status WHERE id = 1`,
		).Scan(&status)
		if err != nil {
			// No row = active (default).
			next(w, r)
			return
		}

		if status == "grace_period" || status == "expired" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{
				"error":  "subscription inactive",
				"status": status,
			})
			return
		}

		next(w, r)
	}
}

type subscriptionStatusRequest struct {
	Status      string `json:"status"`
	GraceEndsAt string `json:"grace_ends_at"`
}

// handleSubscriptionStatusPush receives subscription status updates from the orchestrator.
func (rt *Router) handleSubscriptionStatusPush(w http.ResponseWriter, r *http.Request) {
	var req subscriptionStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	_, err := rt.db.Exec(`
		INSERT INTO subscription_status (id, status, grace_ends_at, updated_at)
		VALUES (1, ?, ?, datetime('now'))
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			grace_ends_at = excluded.grace_ends_at,
			updated_at = datetime('now')`,
		req.Status, req.GraceEndsAt,
	)
	if err != nil {
		log.Printf("subscription status push failed: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handleSubscriptionStatusGet returns the current subscription status for the dashboard.
func (rt *Router) handleSubscriptionStatusGet(w http.ResponseWriter, r *http.Request) {
	var status string
	var graceEndsAt *string
	err := rt.db.QueryRow(
		`SELECT status, grace_ends_at FROM subscription_status WHERE id = 1`,
	).Scan(&status, &graceEndsAt)
	if err != nil {
		// No row yet: treat as active.
		status = "active"
	}

	resp := map[string]string{"status": status}
	if graceEndsAt != nil {
		resp["grace_ends_at"] = *graceEndsAt
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
