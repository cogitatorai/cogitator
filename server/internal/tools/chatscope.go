package tools

import "context"

type chatScopeKey struct{}

// ChatScope carries the user ID, role, and privacy flag for the current chat
// session. It is injected into the context by the agent's Chat method so that
// downstream tool executors can determine memory ownership and authorization.
type ChatScope struct {
	UserID  string
	Role    string // "admin", "moderator", or "user"
	Private bool
}

// WithChatScope returns a new context carrying the given ChatScope.
func WithChatScope(ctx context.Context, s ChatScope) context.Context {
	return context.WithValue(ctx, chatScopeKey{}, s)
}

// ChatScopeFromContext extracts the ChatScope, returning false if absent.
func ChatScopeFromContext(ctx context.Context) (ChatScope, bool) {
	s, ok := ctx.Value(chatScopeKey{}).(ChatScope)
	return s, ok
}
