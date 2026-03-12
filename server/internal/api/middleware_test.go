package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/auth"
)

func TestRequireRole_Allowed(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := requireRole("admin", "member")(inner)

	ctx := auth.WithUser(context.Background(), auth.ContextUser{
		ID:   "user-1",
		Role: "admin",
	})
	req := httptest.NewRequest("GET", "/api/settings", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequireRole_AllowedMember(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := requireRole("admin", "member")(inner)

	ctx := auth.WithUser(context.Background(), auth.ContextUser{
		ID:   "user-2",
		Role: "member",
	})
	req := httptest.NewRequest("GET", "/api/sessions", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequireRole_Forbidden(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := requireRole("admin")(inner)

	ctx := auth.WithUser(context.Background(), auth.ContextUser{
		ID:   "user-3",
		Role: "member",
	})
	req := httptest.NewRequest("DELETE", "/api/users/5", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusForbidden)
	}
	assertJSONError(t, rec, "forbidden")
}

func TestRequireRole_NoUser(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := requireRole("admin")(inner)

	// No user in context at all.
	req := httptest.NewRequest("GET", "/api/settings", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusForbidden)
	}
	assertJSONError(t, rec, "forbidden")
}
