package tools

import (
	"context"
	"encoding/json"
)

// ProgressSink receives streaming output chunks from a tool's execution.
type ProgressSink interface {
	Update(chunk string)
}

// NullSink is a no-op ProgressSink used when no streaming is needed.
type NullSink struct{}

func (NullSink) Update(string) {}

// Result is the return value from a Tool execution.
type Result struct {
	Content string
	IsError bool
	Details any
}

// Tool is the core interface every tool must implement.
type Tool interface {
	Name() string
	Description() string
	InputSchema() json.RawMessage
	Execute(ctx context.Context, in json.RawMessage, sink ProgressSink) (Result, error)
}
