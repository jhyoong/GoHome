package tools

import "context"

type sudoPasswordKey struct{}

// WithSudoPassword stores the sudo password in ctx.
func WithSudoPassword(ctx context.Context, password string) context.Context {
	return context.WithValue(ctx, sudoPasswordKey{}, password)
}

// SudoPasswordFrom retrieves the sudo password stored by WithSudoPassword.
// Returns an empty string if no password is present in ctx.
func SudoPasswordFrom(ctx context.Context) string {
	s, _ := ctx.Value(sudoPasswordKey{}).(string)
	return s
}
