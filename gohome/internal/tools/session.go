package tools

import "context"

// SessionState tracks file access within a single agent session.
type SessionState interface {
	MarkRead(path string)
	HasRead(path string) bool
	CWD() string
}

type sessionKey struct{}

// WithSession stores s in ctx under the session key.
func WithSession(ctx context.Context, s SessionState) context.Context {
	return context.WithValue(ctx, sessionKey{}, s)
}

// SessionFrom retrieves the SessionState stored by WithSession.
// Returns nil if no session is present in ctx.
func SessionFrom(ctx context.Context) SessionState {
	s, _ := ctx.Value(sessionKey{}).(SessionState)
	return s
}
