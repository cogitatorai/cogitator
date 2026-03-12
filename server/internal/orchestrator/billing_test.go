package orchestrator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stripe/stripe-go/v82/webhook"
)

const testWebhookSecret = "whsec_test_secret_for_unit_tests"

// newTestServer creates a Server backed by an in-memory DB for testing.
// It inserts a dummy account and tenant so subscription handlers have
// valid FK targets.
func newTestServer(t *testing.T) *Server {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "billing_test.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Seed account and tenant.
	_, err = db.db.Exec(`INSERT INTO accounts (id, email, password_hash) VALUES ('acct_1', 'test@example.com', 'hash')`)
	if err != nil {
		t.Fatalf("seed account: %v", err)
	}
	now := time.Now().UTC().Format(time.DateTime)
	_, err = db.db.Exec(
		`INSERT INTO tenants (id, account_id, slug, fly_machine_id, fly_volume_id, tier, status, jwt_secret, created_at, updated_at)
		 VALUES ('tenant_1', 'acct_1', 'acme', 'mach_1', 'vol_1', 'free', 'active', 'secret', ?, ?)`,
		now, now,
	)
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	return &Server{
		cfg: Config{
			StripeWebhookSecret: testWebhookSecret,
		},
		db: db,
	}
}

// signedRequest builds an http.Request with a valid Stripe-Signature header
// for the given JSON payload.
func signedRequest(t *testing.T, payload []byte) *http.Request {
	t.Helper()
	signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{
		Payload: payload,
		Secret:  testWebhookSecret,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/billing/webhook", strings.NewReader(string(signed.Payload)))
	req.Header.Set("Stripe-Signature", signed.Header)
	return req
}

// makeSubscriptionEvent builds a raw JSON Stripe event for subscription types.
func makeSubscriptionEvent(eventType, subID, customerID, priceID, status, tenantID string) []byte {
	return []byte(fmt.Sprintf(`{
		"id": "evt_test_123",
		"type": %q,
		"data": {
			"object": {
				"id": %q,
				"customer": %q,
				"status": %q,
				"items": {
					"data": [{"price": {"id": %q}}]
				},
				"current_period_end": %d,
				"metadata": {"tenant_id": %q}
			}
		}
	}`, eventType, subID, customerID, status, priceID,
		time.Now().Add(30*24*time.Hour).Unix(), tenantID))
}

func TestStripeWebhook_SubscriptionCreated_UpdatesTier(t *testing.T) {
	s := newTestServer(t)

	payload := makeSubscriptionEvent(
		"customer.subscription.created",
		"sub_abc", "cus_xyz",
		"price_pro_monthly", "active",
		"tenant_1",
	)

	rec := httptest.NewRecorder()
	s.handleStripeWebhook(rec, signedRequest(t, payload))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify tenant tier updated to pro.
	var tier string
	err := s.db.db.QueryRow(`SELECT tier FROM tenants WHERE id = 'tenant_1'`).Scan(&tier)
	if err != nil {
		t.Fatalf("query tenant: %v", err)
	}
	if tier != "pro" {
		t.Fatalf("expected tier pro, got %s", tier)
	}

	// Verify subscription record created.
	var subTier, subStatus string
	err = s.db.db.QueryRow(`SELECT tier, status FROM subscriptions WHERE stripe_subscription_id = 'sub_abc'`).Scan(&subTier, &subStatus)
	if err != nil {
		t.Fatalf("query subscription: %v", err)
	}
	if subTier != "pro" {
		t.Fatalf("expected subscription tier pro, got %s", subTier)
	}
	if subStatus != "active" {
		t.Fatalf("expected subscription status active, got %s", subStatus)
	}
}

func TestStripeWebhook_SubscriptionCanceled_DowngradesToFree(t *testing.T) {
	s := newTestServer(t)

	// First, create a subscription so there's something to cancel.
	createPayload := makeSubscriptionEvent(
		"customer.subscription.created",
		"sub_cancel_test", "cus_cancel",
		"price_starter_monthly", "active",
		"tenant_1",
	)
	rec := httptest.NewRecorder()
	s.handleStripeWebhook(rec, signedRequest(t, createPayload))
	if rec.Code != http.StatusOK {
		t.Fatalf("setup: expected 200, got %d", rec.Code)
	}

	// Verify tier is starter.
	var tier string
	s.db.db.QueryRow(`SELECT tier FROM tenants WHERE id = 'tenant_1'`).Scan(&tier)
	if tier != "starter" {
		t.Fatalf("setup: expected tier starter, got %s", tier)
	}

	// Now cancel.
	cancelPayload := makeSubscriptionEvent(
		"customer.subscription.deleted",
		"sub_cancel_test", "cus_cancel",
		"price_starter_monthly", "canceled",
		"tenant_1",
	)
	rec = httptest.NewRecorder()
	s.handleStripeWebhook(rec, signedRequest(t, cancelPayload))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Verify tenant downgraded to free.
	s.db.db.QueryRow(`SELECT tier FROM tenants WHERE id = 'tenant_1'`).Scan(&tier)
	if tier != "free" {
		t.Fatalf("expected tier free after cancellation, got %s", tier)
	}

	// Verify subscription status is canceled.
	var status string
	s.db.db.QueryRow(`SELECT status FROM subscriptions WHERE stripe_subscription_id = 'sub_cancel_test'`).Scan(&status)
	if status != "canceled" {
		t.Fatalf("expected subscription status canceled, got %s", status)
	}
}

func TestStripeWebhook_InvalidSignature_Returns400(t *testing.T) {
	s := newTestServer(t)

	body := `{"id":"evt_bad","type":"customer.subscription.created","data":{"object":{}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/billing/webhook", strings.NewReader(body))
	req.Header.Set("Stripe-Signature", "t=12345,v1=badsignature")

	rec := httptest.NewRecorder()
	s.handleStripeWebhook(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestStripeWebhook_InvoicePaymentFailed_Returns200(t *testing.T) {
	s := newTestServer(t)

	payload, _ := json.Marshal(map[string]any{
		"id":   "evt_payment_fail",
		"type": "invoice.payment_failed",
		"data": map[string]any{
			"object": map[string]any{
				"id":       "in_123",
				"customer": "cus_123",
			},
		},
	})

	rec := httptest.NewRecorder()
	s.handleStripeWebhook(rec, signedRequest(t, payload))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStripeWebhook_MissingTenantID_NoDBChange(t *testing.T) {
	s := newTestServer(t)

	// Subscription with no tenant_id in metadata.
	payload := []byte(fmt.Sprintf(`{
		"id": "evt_no_tenant",
		"type": "customer.subscription.created",
		"data": {
			"object": {
				"id": "sub_orphan",
				"customer": "cus_orphan",
				"status": "active",
				"items": {"data": [{"price": {"id": "price_pro_monthly"}}]},
				"current_period_end": %d,
				"metadata": {}
			}
		}
	}`, time.Now().Add(30*24*time.Hour).Unix()))

	rec := httptest.NewRecorder()
	s.handleStripeWebhook(rec, signedRequest(t, payload))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Tenant tier should remain free (unchanged).
	var tier string
	s.db.db.QueryRow(`SELECT tier FROM tenants WHERE id = 'tenant_1'`).Scan(&tier)
	if tier != "free" {
		t.Fatalf("expected tier unchanged (free), got %s", tier)
	}
}
