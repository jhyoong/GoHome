package tools

import "context"

type toolUseIDKey struct{}

// WithToolUseID stores the ID of the tool_use block being executed in ctx.
func WithToolUseID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, toolUseIDKey{}, id)
}

// ToolUseIDFrom retrieves the ID stored by WithToolUseID.
// Returns an empty string if no ID is present in ctx.
func ToolUseIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(toolUseIDKey{}).(string)
	return id
}
