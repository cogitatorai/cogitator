// Package reqctx carries request-scoped correlation values (request ID,
// session key) in a context.Context. It lives in its own low-level package so
// any layer (api middleware, memory retriever, agent) can read or set them
// without import cycles.
package reqctx

import "context"

type ctxKey int

const (
	requestIDKey ctxKey = iota
	sessionKeyKey
)

// WithRequestID returns a context carrying the per-request ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID returns the request ID, or "" when absent.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// WithSessionKey returns a context carrying the chat session key.
func WithSessionKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, sessionKeyKey, key)
}

// SessionKey returns the session key, or "" when absent.
func SessionKey(ctx context.Context) string {
	k, _ := ctx.Value(sessionKeyKey).(string)
	return k
}
