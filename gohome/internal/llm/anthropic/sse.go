package anthropic

import (
	"context"
	"io"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// sseFrame holds a single SSE event frame with its event name and data payload.
type sseFrame struct {
	event string
	data  string
}

// parseSSE adapts common.ParseSSE. A read error becomes an "error" frame.
func parseSSE(ctx context.Context, r io.Reader) <-chan sseFrame {
	ch := make(chan sseFrame, 16)
	go func() {
		defer close(ch)
		for f := range common.ParseSSE(ctx, r) {
			out := sseFrame{event: f.Event, data: f.Data}
			if f.Err != nil {
				out = sseFrame{event: "error", data: f.Err.Error()}
			}
			select {
			case ch <- out:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}
