package auth

import "context"

type contextKey string

const userContextKey contextKey = "user"

// ContextUser holds the authenticated user's identity within a request context.
type ContextUser struct {
	ID   string
	Role string
}

// WithUser returns a new context carrying the given user.
func WithUser(ctx context.Context, u ContextUser) context.Context {
	return context.WithValue(ctx, userContextKey, u)
}

// UserFromContext extracts the user from context, returning false if absent.
func UserFromContext(ctx context.Context) (ContextUser, bool) {
	u, ok := ctx.Value(userContextKey).(ContextUser)
	return u, ok
}

// MustUserFromContext extracts the user or panics. Use only in code paths
// where authentication middleware has already run.
func MustUserFromContext(ctx context.Context) ContextUser {
	u, ok := UserFromContext(ctx)
	if !ok {
		panic("no user in context")
	}
	return u
}
