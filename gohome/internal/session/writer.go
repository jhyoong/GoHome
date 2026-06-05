package session

import (
	"os"
	"path/filepath"
	"sync"
)

// Writer asynchronously writes JSONL events to a file.
type Writer struct {
	f         *os.File
	ch        chan any
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
}

// OpenWriter opens (or creates) the JSONL file at path, creating parent directories
// as needed, and starts the background write goroutine.
func OpenWriter(path string) (*Writer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	w := &Writer{
		f:    f,
		ch:   make(chan any, 64),
		done: make(chan struct{}),
	}
	go w.run()
	return w, nil
}

// Emit sends ev to the background writer. It does not block unless the
// channel buffer (64) is full.
func (w *Writer) Emit(ev any) {
	w.ch <- ev
}

// Close flushes all queued events and closes the file.
// It is idempotent: the second and subsequent calls are no-ops that return nil.
func (w *Writer) Close() error {
	w.closeOnce.Do(func() {
		close(w.ch)
		<-w.done
		w.closeErr = w.f.Close()
	})
	return w.closeErr
}

// isCritical reports whether ev requires an fsync after writing.
func isCritical(ev any) bool {
	switch ev.(type) {
	case SessionStart, SessionEnd, Approval:
		return true
	}
	return false
}

func (w *Writer) run() {
	defer close(w.done)
	for ev := range w.ch {
		line, err := Encode(ev)
		if err != nil {
			// Skip unencodable events rather than crashing the goroutine.
			continue
		}
		_, _ = w.f.Write(line)
		_, _ = w.f.Write([]byte("\n"))
		if isCritical(ev) {
			_ = w.f.Sync()
		}
	}
}
