package openai

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

// sseFrame holds a single SSE data line payload from an OpenAI stream.
// OpenAI SSE has no "event:" lines — only "data:" lines matter.
// When done is true, the data field contains "[DONE]" (the sentinel value).
// An error frame has data set to an error description string; done is false.
type sseFrame struct {
	data string
	done bool
	err  error
}

// parseSSE reads an OpenAI SSE stream from r and emits one sseFrame per
// non-empty data line. The [DONE] sentinel is emitted as a frame with done==true.
// Comment lines (starting with ':') and blank lines are skipped.
// The channel is closed at EOF or when ctx is cancelled.
// If the scanner reports an error after the scan loop, an error frame is emitted.
// The internal scanner buffer supports lines up to 1 MiB.
func parseSSE(ctx context.Context, r io.Reader) <-chan sseFrame {
	ch := make(chan sseFrame, 16)
	go func() {
		defer close(ch)

		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		send := func(f sseFrame) bool {
			select {
			case ch <- f:
				return true
			case <-ctx.Done():
				return false
			}
		}

		for scanner.Scan() {
			line := scanner.Text()

			// blank line: skip (OpenAI uses blank lines as frame separators,
			// but each data: line is its own self-contained frame).
			if line == "" {
				continue
			}

			// comment line
			if strings.HasPrefix(line, ":") {
				continue
			}

			if strings.HasPrefix(line, "data:") {
				payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if payload == "[DONE]" {
					send(sseFrame{done: true})
					return
				}
				if !send(sseFrame{data: payload}) {
					return
				}
			}
			// Other field types (id:, retry:, event:) are ignored.
		}

		if err := scanner.Err(); err != nil {
			send(sseFrame{err: fmt.Errorf("parseSSE: %w", err)})
		}
	}()
	return ch
}
