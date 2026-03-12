package orchestrator

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/orchestrator/cloudflare"
	"github.com/cogitatorai/cogitator/server/internal/orchestrator/fly"
)

// stubFlyClient implements fly.MachineAPI and always returns an error,
// allowing tests to reach the Deprovision call without panicking.
type stubFlyClient struct{}

func (stubFlyClient) CreateVolume(string, int, string) (*fly.Volume, error) {
	return nil, errors.New("stub")
}
func (stubFlyClient) DeleteVolume(string) error            { return errors.New("stub") }
func (stubFlyClient) CreateMachine(fly.MachineConfig, string) (*fly.Machine, error) {
	return nil, errors.New("stub")
}
func (stubFlyClient) UpdateMachine(string, string) error { return errors.New("stub") }
func (stubFlyClient) StartMachine(string) error          { return errors.New("stub") }
func (stubFlyClient) StopMachine(string) error           { return errors.New("stub") }
func (stubFlyClient) DestroyMachine(string) error        { return errors.New("stub") }
func (stubFlyClient) GetMachine(string) (*fly.Machine, error) {
	return nil, errors.New("stub")
}

// stubDNSClient implements cloudflare.DNSAPI and always returns an error.
type stubDNSClient struct{}

func (stubDNSClient) AddCNAME(string, string, bool) (*cloudflare.DNSRecord, error) {
	return nil, errors.New("stub")
}
func (stubDNSClient) FindRecord(string) (*cloudflare.DNSRecord, error) { return nil, errors.New("stub") }
func (stubDNSClient) DeleteRecord(string) error                        { return errors.New("stub") }

// newOperatorTestServer creates a minimal Server backed by a fresh DB for
// operator-related tests. Distinct from newTestServer in billing_test.go.
func newOperatorTestServer(t *testing.T) *Server {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "op_test.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	srv := &Server{
		cfg:       Config{InternalSecret: "test-internal-secret"},
		db:        db,
		jwtSecret: []byte("test-jwt-secret"),
	}
	srv.router = srv.buildRouter()
	return srv
}

// seedAccount inserts an account and returns (accountID, token).
func seedAccount(t *testing.T, s *Server, email string, isOperator bool) (string, string) {
	t.Helper()
	accountID := "acct_" + email
	_, err := s.db.db.Exec(
		`INSERT INTO accounts (id, email, password_hash, is_operator) VALUES (?, ?, 'hash', ?)`,
		accountID, email, isOperator,
	)
	if err != nil {
		t.Fatalf("seed account %s: %v", email, err)
	}
	token, err := s.generateToken(accountID, email)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return accountID, token
}

func TestRequireAuthInjectsOperatorStatus(t *testing.T) {
	s := newOperatorTestServer(t)

	// Seed a regular account and an operator account.
	_, regularToken := seedAccount(t, s, "user@example.com", false)
	_, opToken := seedAccount(t, s, "operator@example.com", true)

	tests := []struct {
		name       string
		token      string
		wantIsOp   bool
	}{
		{"regular user", regularToken, false},
		{"operator user", opToken, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotIsOp bool
			probe := s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
				gotIsOp, _ = r.Context().Value(ctxIsOperator).(bool)
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			rec := httptest.NewRecorder()
			probe(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rec.Code)
			}
			if gotIsOp != tc.wantIsOp {
				t.Fatalf("expected isOperator=%v, got %v", tc.wantIsOp, gotIsOp)
			}
		})
	}
}

func TestRequireAuthRejectsMissingToken(t *testing.T) {
	s := newOperatorTestServer(t)

	probe := s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	probe(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRequireOperatorAllowsOperator(t *testing.T) {
	s := newOperatorTestServer(t)
	_, opToken := seedAccount(t, s, "op@example.com", true)

	probe := s.requireOperator(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+opToken)
	rec := httptest.NewRecorder()
	probe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for operator, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRequireOperatorRejectsNonOperator(t *testing.T) {
	s := newOperatorTestServer(t)
	_, regularToken := seedAccount(t, s, "user@example.com", false)

	probe := s.requireOperator(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+regularToken)
	rec := httptest.NewRecorder()
	probe(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-operator, got %d", rec.Code)
	}
}

func TestCORSPreflightResponse(t *testing.T) {
	s := newOperatorTestServer(t)

	handler := corsMiddleware(s.router)
	req := httptest.NewRequest(http.MethodOptions, "/api/health", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "GET")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for OPTIONS preflight, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("expected Access-Control-Allow-Origin: *, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("missing Access-Control-Allow-Methods header")
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Fatal("missing Access-Control-Allow-Headers header")
	}
}

func TestCORSHeadersOnNormalRequest(t *testing.T) {
	s := newOperatorTestServer(t)

	handler := corsMiddleware(s.router)
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("expected CORS header on normal request, got %q", got)
	}
}

func TestOperatorCanDeleteAnyTenant(t *testing.T) {
	s := newOperatorTestServer(t)

	// Seed owner account and a different operator account.
	ownerID, _ := seedAccount(t, s, "owner@example.com", false)
	operatorID, _ := seedAccount(t, s, "op@example.com", true)

	// Insert a tenant owned by the owner.
	_, err := s.db.db.Exec(
		`INSERT INTO tenants (id, account_id, slug, jwt_secret) VALUES ('t_op_delete', ?, 'op-delete-slug', 'secret')`,
		ownerID,
	)
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// Build a request context that simulates what requireAuth injects for an operator.
	ctx := context.WithValue(context.Background(), ctxAccountID, operatorID)
	ctx = context.WithValue(ctx, ctxIsOperator, true)

	req := httptest.NewRequest(http.MethodDelete, "/api/tenants/t_op_delete", nil)
	req = req.WithContext(ctx)
	req.SetPathValue("id", "t_op_delete")
	rec := httptest.NewRecorder()

	// Use stub clients so Deprovision returns an error (500) rather than panic.
	// A 500 confirms the ownership check passed; a 403 would mean it didn't.
	s.provisioner = NewTenantProvisioner(s.db, stubFlyClient{}, stubDNSClient{}, "", "", "", "")
	s.handleDeleteTenant(rec, req)

	// Operator should pass the ownership check; Deprovision will fail with no
	// Fly client (500), but that is not a 403 Forbidden.
	if rec.Code == http.StatusForbidden {
		t.Fatalf("operator should not receive 403, got %d: %s", rec.Code, rec.Body.String())
	}
}
