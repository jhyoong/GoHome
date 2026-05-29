package anthropic

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

// sseFrame holds a single SSE event frame with its event name and data payload.
type sseFrame struct {
	event string
	data  string
}

// parseSSE reads SSE from r and emits one sseFrame per blank-line-terminated block.
// Comment lines (starting with ':') are skipped.
// Multiple data: lines in one block are joined with "\n".
// The channel is closed at EOF or when ctx is cancelled.
// If scanner.Err() is non-nil after the scan loop, an error frame is emitted instead
// of any partially-accumulated trailing frame.
func parseSSE(ctx context.Context, r io.Reader) <-chan sseFrame {
	ch := make(chan sseFrame, 16)
	go func() {
		defer close(ch)

		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		var event string
		var dataParts []string

		send := func(f sseFrame) bool {
			select {
			case ch <- f:
				return true
			case <-ctx.Done():
				return false
			}
		}

		emit := func() bool {
			if event == "" && len(dataParts) == 0 {
				return true
			}
			f := sseFrame{
				event: event,
				data:  strings.Join(dataParts, "\n"),
			}
			event = ""
			dataParts = dataParts[:0]
			return send(f)
		}

		for scanner.Scan() {
			line := scanner.Text()

			// blank line: end of frame
			if line == "" {
				if !emit() {
					return
				}
				continue
			}

			// comment line
			if strings.HasPrefix(line, ":") {
				continue
			}

			if strings.HasPrefix(line, "event:") {
				event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			} else if strings.HasPrefix(line, "data:") {
				dataParts = append(dataParts, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}

		if err := scanner.Err(); err != nil {
			// I/O error or line-too-long: discard any partial frame and emit an error frame.
			send(sseFrame{event: "error", data: fmt.Sprintf("parseSSE: %v", err)})
			return
		}

		// Clean EOF: emit any trailing frame (no trailing blank line).
		emit()
	}()
	return ch
}
