package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/metrics"
)

func TestRequestIDHeaderAndContext(t *testing.T) {
	var seenID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenID = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	handler := requestLogMiddleware(nil)(routeCaptureMiddleware(inner))

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	headerID := w.Header().Get("X-Request-Id")
	if headerID == "" {
		t.Fatal("X-Request-Id header not set")
	}
	if seenID != headerID {
		t.Errorf("context ID %q != header ID %q", seenID, headerID)
	}
}

func TestRequestLogRecordsRouteInRing(t *testing.T) {
	ring := metrics.NewRing(10)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/widgets/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	handler := requestLogMiddleware(ring)(routeCaptureMiddleware(mux))

	req := httptest.NewRequest("GET", "/api/widgets/42", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	snap := ring.Snapshot()
	stats, ok := snap.Routes["GET /api/widgets/{id}"]
	if !ok {
		t.Fatalf("route pattern not recorded; routes: %v", snap.Routes)
	}
	if stats.RequestCount != 1 {
		t.Errorf("stats = %+v", stats)
	}
}

func TestRequestLogOmitsQueryString(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := requestLogMiddleware(nil)(routeCaptureMiddleware(inner))

	req := httptest.NewRequest("GET", "/ws?token=supersecretjwt", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if strings.Contains(buf.String(), "supersecretjwt") {
		t.Errorf("query string leaked into log: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "/ws") {
		t.Errorf("path not logged: %s", buf.String())
	}
}

func TestRouterSetsRequestID(t *testing.T) {
	router := setupTestRouter(t)
	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Header().Get("X-Request-Id") == "" {
		t.Error("router chain does not set X-Request-Id")
	}
}
