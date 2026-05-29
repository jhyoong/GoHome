package anthropic

import (
	"bufio"
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
// The channel is closed at EOF.
func parseSSE(r io.Reader) <-chan sseFrame {
	ch := make(chan sseFrame, 16)
	go func() {
		defer close(ch)

		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		var event string
		var dataParts []string

		emit := func() {
			if event == "" && len(dataParts) == 0 {
				return
			}
			ch <- sseFrame{
				event: event,
				data:  strings.Join(dataParts, "\n"),
			}
			event = ""
			dataParts = dataParts[:0]
		}

		for scanner.Scan() {
			line := scanner.Text()

			// blank line: end of frame
			if line == "" {
				emit()
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

		// emit any trailing frame (no trailing blank line)
		emit()
	}()
	return ch
}
