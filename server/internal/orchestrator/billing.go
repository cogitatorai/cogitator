package orchestrator

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	stripe "github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/webhook"
)

// priceToTier maps Stripe price IDs to internal tier names.
// These will be populated with real price IDs once Stripe products are configured.
var priceToTier = map[string]string{
	"price_starter_monthly": "starter",
	"price_starter_yearly":  "starter",
	"price_pro_monthly":     "pro",
	"price_pro_yearly":      "pro",
}

// handleStripeWebhook receives Stripe webhook events and processes
// subscription lifecycle changes. Authentication is via Stripe's
// webhook signature (not JWT or internal secret).
func (s *Server) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 65536))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	event, err := webhook.ConstructEventWithOptions(body, r.Header.Get("Stripe-Signature"), s.cfg.StripeWebhookSecret, webhook.ConstructEventOptions{
		IgnoreAPIVersionMismatch: true,
	})
	if err != nil {
		http.Error(w, "invalid signature", http.StatusBadRequest)
		return
	}

	switch event.Type {
	case stripe.EventTypeCustomerSubscriptionCreated,
		stripe.EventTypeCustomerSubscriptionUpdated:
		s.handleSubscriptionChange(event)
	case stripe.EventTypeCustomerSubscriptionDeleted:
		s.handleSubscriptionCanceled(event)
	case stripe.EventTypeInvoicePaymentFailed:
		log.Printf("billing: payment failed for event %s", event.ID)
	}

	w.WriteHeader(http.StatusOK)
}

// subscriptionData holds the fields we extract from a Stripe subscription object.
type subscriptionData struct {
	ID                 string
	CustomerID         string
	Status             string
	PriceID            string
	CurrentPeriodEnd   time.Time
	MetadataTenantID   string
}

// parseSubscription extracts subscription fields from the raw event data.
func parseSubscription(event stripe.Event) (*subscriptionData, error) {
	var raw struct {
		ID       string `json:"id"`
		Customer string `json:"customer"`
		Status   string `json:"status"`
		Items    struct {
			Data []struct {
				Price struct {
					ID string `json:"id"`
				} `json:"price"`
			} `json:"data"`
		} `json:"items"`
		CurrentPeriodEnd int64             `json:"current_period_end"`
		Metadata         map[string]string `json:"metadata"`
	}

	if err := json.Unmarshal(event.Data.Raw, &raw); err != nil {
		return nil, err
	}

	var priceID string
	if len(raw.Items.Data) > 0 {
		priceID = raw.Items.Data[0].Price.ID
	}

	return &subscriptionData{
		ID:               raw.ID,
		CustomerID:       raw.Customer,
		Status:           raw.Status,
		PriceID:          priceID,
		CurrentPeriodEnd: time.Unix(raw.CurrentPeriodEnd, 0).UTC(),
		MetadataTenantID: raw.Metadata["tenant_id"],
	}, nil
}

// handleSubscriptionChange processes subscription created/updated events.
// It upserts the subscription record and updates the tenant tier.
func (s *Server) handleSubscriptionChange(event stripe.Event) {
	sub, err := parseSubscription(event)
	if err != nil {
		log.Printf("billing: failed to parse subscription: %v", err)
		return
	}

	tier := priceToTier[sub.PriceID]
	if tier == "" {
		tier = "free"
	}

	tenantID := sub.MetadataTenantID
	if tenantID == "" {
		log.Printf("billing: subscription %s has no tenant_id in metadata", sub.ID)
		return
	}

	now := time.Now().UTC().Format(time.DateTime)
	periodEnd := sub.CurrentPeriodEnd.Format(time.DateTime)

	_, err = s.db.db.Exec(`
		INSERT INTO subscriptions (id, tenant_id, stripe_customer_id, stripe_subscription_id, tier, status, current_period_end, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			tier = excluded.tier,
			status = excluded.status,
			current_period_end = excluded.current_period_end`,
		sub.ID, tenantID, sub.CustomerID, sub.ID, tier, sub.Status, periodEnd, now,
	)
	if err != nil {
		log.Printf("billing: failed to upsert subscription %s: %v", sub.ID, err)
		return
	}

	_, err = s.db.db.Exec(
		`UPDATE tenants SET tier = ?, updated_at = ? WHERE id = ?`,
		tier, now, tenantID,
	)
	if err != nil {
		log.Printf("billing: failed to update tenant %s tier: %v", tenantID, err)
		return
	}

	log.Printf("billing: tenant %s updated to tier %q via subscription %s", tenantID, tier, sub.ID)
}

// handleSubscriptionCanceled processes subscription deleted events.
// It marks the subscription as canceled and downgrades the tenant to free tier.
func (s *Server) handleSubscriptionCanceled(event stripe.Event) {
	sub, err := parseSubscription(event)
	if err != nil {
		log.Printf("billing: failed to parse canceled subscription: %v", err)
		return
	}

	tenantID := sub.MetadataTenantID
	if tenantID == "" {
		log.Printf("billing: canceled subscription %s has no tenant_id in metadata", sub.ID)
		return
	}

	now := time.Now().UTC().Format(time.DateTime)

	_, err = s.db.db.Exec(
		`UPDATE subscriptions SET status = 'canceled' WHERE stripe_subscription_id = ?`,
		sub.ID,
	)
	if err != nil {
		log.Printf("billing: failed to cancel subscription %s: %v", sub.ID, err)
		return
	}

	_, err = s.db.db.Exec(
		`UPDATE tenants SET tier = 'free', updated_at = ? WHERE id = ?`,
		now, tenantID,
	)
	if err != nil {
		log.Printf("billing: failed to downgrade tenant %s: %v", tenantID, err)
		return
	}

	log.Printf("billing: tenant %s downgraded to free (subscription %s canceled)", tenantID, sub.ID)
}
