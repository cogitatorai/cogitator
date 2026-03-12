package orchestrator

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/orchestrator/cloudflare"
	"github.com/cogitatorai/cogitator/server/internal/orchestrator/fly"
	"github.com/cogitatorai/cogitator/server/internal/ratelimit"
)

// Server is the orchestrator HTTP service.
type Server struct {
	cfg            Config
	db             *OrchestratorDB
	router         *http.ServeMux
	provisioner    *TenantProvisioner
	rolloutManager *RolloutManager
	jwtSecret      []byte
	signupLimiter  *ratelimit.Limiter
}

// NewServer creates a new orchestrator server, opening the database and
// building the HTTP router.
func NewServer(cfg Config) (*Server, error) {
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		return nil, err
	}

	if opEmail := os.Getenv("ORCHESTRATOR_OPERATOR_EMAIL"); opEmail != "" {
		if err := db.PromoteOperator(opEmail); err != nil {
			log.Printf("operator promotion: %v (account may not exist yet)", err)
		}
	}

	flyClient := fly.NewClient(cfg.FlyAPIToken, cfg.FlyAppName)
	cfClient := cloudflare.NewClient(cfg.CloudflareToken, cfg.CloudflareZoneID)
	provisioner := NewTenantProvisioner(
		db, flyClient, cfClient,
		"cdg",
		"registry.fly.io/cogitator-saas:latest",
		cfg.InternalSecret,
		cfg.FlyAppName,
	)

	rolloutMgr := NewRolloutManager(db, flyClient, cfg.InternalSecret)

	// Use a dedicated JWT secret, falling back to a random value if not configured.
	jwtSecret := cfg.JWTSecret
	if jwtSecret == "" {
		generated, err := randomHex(32)
		if err != nil {
			return nil, err
		}
		jwtSecret = generated
		log.Printf("warning: ORCHESTRATOR_JWT_SECRET not set, using random secret (JWTs will not survive restarts)")
	}

	s := &Server{
		cfg:            cfg,
		db:             db,
		provisioner:    provisioner,
		rolloutManager: rolloutMgr,
		jwtSecret:      []byte(jwtSecret),
		signupLimiter:  ratelimit.New(10),
	}
	s.router = s.buildRouter()
	return s, nil
}

// corsMiddleware adds CORS headers to every response and short-circuits
// OPTIONS preflight requests with 204 No Content.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) buildRouter() *http.ServeMux {
	mux := http.NewServeMux()

	// Health check.
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Public (no auth).
	mux.HandleFunc("POST /api/signup", s.handleSignup)
	mux.HandleFunc("POST /api/login", s.handleLogin)

	// Authenticated (orchestrator JWT).
	mux.HandleFunc("POST /api/tenants", s.requireAuth(s.handleCreateTenant))
	mux.HandleFunc("DELETE /api/tenants/{id}", s.requireAuth(s.handleDeleteTenant))
	mux.HandleFunc("GET /api/tenants/{id}/status", s.requireAuth(s.handleTenantStatus))

	// Operator endpoints (require is_operator).
	mux.HandleFunc("GET /api/tenants", s.requireOperator(s.handleListTenants))
	mux.HandleFunc("GET /api/fleet/stats", s.requireOperator(s.handleFleetStats))

	mux.HandleFunc("GET /api/tenants/{id}", s.requireOperator(s.handleTenantDetail))
	mux.HandleFunc("GET /api/tenants/{id}/heartbeats", s.requireOperator(s.handleTenantHeartbeats))
	mux.HandleFunc("GET /api/releases", s.requireOperator(s.handleListReleases))
	mux.HandleFunc("GET /api/rollouts", s.requireOperator(s.handleListRollouts))
	mux.HandleFunc("GET /api/rollouts/{id}", s.requireOperator(s.handleRolloutDetail))
	mux.HandleFunc("POST /api/rollouts/{id}/rollback", s.requireOperator(s.handleRollback))

	// Stripe webhook (authenticated via webhook signature, not JWT).
	mux.HandleFunc("POST /api/billing/webhook", s.handleStripeWebhook)

	// Internal (from tenant machines, X-Internal-Secret).
	mux.HandleFunc("POST /api/internal/heartbeat", s.requireInternal(s.handleHeartbeat))
	mux.HandleFunc("POST /api/internal/schedule-wake", s.requireInternal(s.handleScheduleWake))
	mux.HandleFunc("POST /api/internal/releases", s.requireInternal(s.handleNewRelease))
	mux.HandleFunc("POST /api/internal/rollouts/{id}/rollback", s.requireInternal(s.handleRollback))

	return mux
}

// Run starts the HTTP server and blocks until a SIGINT or SIGTERM is received,
// then shuts down gracefully.
func (s *Server) Run() error {
	waker := NewWaker(s.db)
	waker.Start()
	defer waker.Stop()

	httpServer := &http.Server{
		Addr:    net.JoinHostPort("", s.cfg.Port),
		Handler: corsMiddleware(s.router),
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("orchestrator listening on :%s", s.cfg.Port)
		errCh <- httpServer.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-stop:
		log.Printf("received %s, shutting down...", sig)
	case err := <-errCh:
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
	}

	// Wait for in-flight rollouts to complete before closing the database.
	s.rolloutManager.Shutdown()

	s.db.Close()
	return nil
}
