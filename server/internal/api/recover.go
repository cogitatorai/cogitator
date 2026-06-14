package api

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
)

// recoverResponseWriter wraps an http.ResponseWriter to track whether any
// response has been committed (status line or body written). This lets the
// recovery middleware decide whether it can still emit a clean 500 or must
// only log because the response is already partially written.
//
// It forwards Hijack and Flush so WebSocket upgrades and streaming handlers
// continue to work when this wrapper is applied at the outermost layer.
type recoverResponseWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *recoverResponseWriter) WriteHeader(code int) {
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *recoverResponseWriter) Write(b []byte) (int, error) {
	w.wroteHeader = true
	return w.ResponseWriter.Write(b)
}

// Hijack implements http.Hijacker so WebSocket upgrades work through this wrapper.
func (w *recoverResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		w.wroteHeader = true
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not implement http.Hijacker")
}

// Flush implements http.Flusher for streaming handlers.
func (w *recoverResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// recoverMiddleware catches panics from any handler or inner middleware. On
// panic it logs the request method, path, and stack trace via slog and, if no
// response has been committed yet, returns a standard JSON 500 error. If the
// response was already partially written, it only logs (it must not double-write
// the header). http.ErrAbortHandler is re-panicked, as the net/http server
// treats it as a sanctioned silent abort; swallowing it would break
// http.ServeContent and reverse-proxy behavior.
func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &recoverResponseWriter{ResponseWriter: w}

		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler {
				panic(rec)
			}

			// The request ID is read back from the response header because
			// this middleware sits outside requestLogMiddleware: the ID is
			// set on the shared header map before the inner chain runs.
			slog.Error("recovered from handler panic",
				"request_id", rw.Header().Get("X-Request-Id"),
				"method", r.Method,
				"path", r.URL.Path,
				"panic", rec,
				"stack", string(debug.Stack()),
			)

			if !rw.wroteHeader {
				writeError(rw, http.StatusInternalServerError, "internal server error")
			}
		}()

		next.ServeHTTP(rw, r)
	})
}
