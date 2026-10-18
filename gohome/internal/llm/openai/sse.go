package openai

import (
	"context"
	"io"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// sseFrame holds a single SSE data payload from an OpenAI stream.
// When done is true, the stream ended with the "[DONE]" sentinel.
// When err is set, the stream failed while reading.
type sseFrame struct {
	data string
	done bool
	err  error
}

// parseSSE adapts common.ParseSSE. Events with no data are skipped and the
// [DONE] sentinel ends the stream with a frame that has done set.
func parseSSE(ctx context.Context, r io.Reader) <-chan sseFrame {
	ch := make(chan sseFrame, 16)
	go func() {
		defer close(ch)
		for f := range common.ParseSSE(ctx, r) {
			var out sseFrame
			switch {
			case f.Err != nil:
				out = sseFrame{err: f.Err}
			case f.Data == "":
				continue
			case f.Data == "[DONE]":
				out = sseFrame{done: true}
			default:
				out = sseFrame{data: f.Data}
			}
			select {
			case ch <- out:
			case <-ctx.Done():
				return
			}
			if out.done {
				return
			}
		}
	}()
	return ch
}
