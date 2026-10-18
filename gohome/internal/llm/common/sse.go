package common

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
)

// SSEFrame is one server-sent event: the event name, the data payload, or a
// read error. Err is set only on the last frame of a stream that failed.
type SSEFrame struct {
	Event string
	Data  string
	Err   error
}

// splitSSELines is a bufio.SplitFunc that accepts LF, CRLF and bare CR as
// line endings.
func splitSSELines(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	i := bytes.IndexAny(data, "\r\n")
	if i < 0 {
		if atEOF {
			return len(data), data, nil
		}
		return 0, nil, nil
	}
	if data[i] == '\n' {
		return i + 1, data[:i], nil
	}
	// data[i] is CR. A following LF belongs to the same line ending.
	if i+1 < len(data) {
		if data[i+1] == '\n' {
			return i + 2, data[:i], nil
		}
		return i + 1, data[:i], nil
	}
	if atEOF {
		return i + 1, data[:i], nil
	}
	// CR is the last byte read; wait to see whether an LF follows.
	return 0, nil, nil
}

// ParseSSE reads SSE from r and emits one SSEFrame per blank-line-terminated
// event. Comment lines (starting with ':') are skipped. Multiple data: lines
// in one event are joined with "\n". The channel is closed at EOF or when ctx
// is cancelled. On a read error, any partial event is dropped and a frame with
// Err set is emitted last.
func ParseSSE(ctx context.Context, r io.Reader) <-chan SSEFrame {
	ch := make(chan SSEFrame, 16)
	go func() {
		defer close(ch)

		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		scanner.Split(splitSSELines)

		var event string
		var dataParts []string

		send := func(f SSEFrame) bool {
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
			f := SSEFrame{Event: event, Data: strings.Join(dataParts, "\n")}
			event = ""
			dataParts = dataParts[:0]
			return send(f)
		}

		for scanner.Scan() {
			line := scanner.Text()

			if line == "" {
				if !emit() {
					return
				}
				continue
			}
			if strings.HasPrefix(line, ":") {
				continue
			}
			if v, ok := strings.CutPrefix(line, "event:"); ok {
				event = strings.TrimSpace(v)
			} else if v, ok := strings.CutPrefix(line, "data:"); ok {
				dataParts = append(dataParts, strings.TrimPrefix(v, " "))
			}
		}

		if err := scanner.Err(); err != nil {
			send(SSEFrame{Err: fmt.Errorf("parseSSE: %w", err)})
			return
		}

		// Clean EOF: emit any trailing event with no closing blank line.
		emit()
	}()
	return ch
}
