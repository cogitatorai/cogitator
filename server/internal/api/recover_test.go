package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecoverMiddleware_PanicReturns500JSON(t *testing.T) {
	panicked := false
	inner := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	})

	handler := recoverMiddleware(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/explode", nil)
	rec := httptest.NewRecorder()

	// The middleware must absorb the panic so the surrounding call returns
	// normally rather than crashing the server.
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		handler.ServeHTTP(rec, req)
	}()

	if panicked {
		t.Fatal("panic escaped recoverMiddleware")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got status %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("got Content-Type %q, want application/json", ct)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON: %v (body=%q)", err, rec.Body.String())
	}
	if body["error"] == "" {
		t.Fatalf("expected non-empty error field, got %v", body)
	}
}

func TestRecoverMiddleware_RepanicsErrAbortHandler(t *testing.T) {
	inner := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic(http.ErrAbortHandler)
	})

	handler := recoverMiddleware(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/abort", nil)
	rec := httptest.NewRecorder()

	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		handler.ServeHTTP(rec, req)
	}()

	if recovered != http.ErrAbortHandler {
		t.Fatalf("expected http.ErrAbortHandler to be re-panicked, got %v", recovered)
	}
}

func TestRecoverMiddleware_NoPanicPassesThrough(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("ok"))
	})

	handler := recoverMiddleware(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/fine", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("got status %d, want %d", rec.Code, http.StatusTeapot)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("got body %q, want %q", rec.Body.String(), "ok")
	}
}

func TestRecoverMiddleware_PanicAfterPartialWriteDoesNotDoubleWrite(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		panic("boom after write")
	})

	handler := recoverMiddleware(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/partial", nil)
	rec := httptest.NewRecorder()

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic escaped recoverMiddleware: %v", r)
			}
		}()
		handler.ServeHTTP(rec, req)
	}()

	// Status was already committed to 200 by the handler; recover must not
	// overwrite it with 500, and must not append a second JSON error body.
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d (recover should not re-write after partial write)", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "partial" {
		t.Fatalf("got body %q, want %q (recover must not append after partial write)", rec.Body.String(), "partial")
	}
}
