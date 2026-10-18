package session

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// Writer asynchronously writes JSONL events to a file.
type Writer struct {
	f         *os.File
	out       io.Writer // f, replaced in tests to simulate write failures
	ch        chan any
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error

	// mu guards closed: Emit holds the read lock while sending so Close
	// cannot close ch underneath it.
	mu     sync.RWMutex
	closed bool

	// errMu guards err, the first write, encode or sync error.
	errMu sync.Mutex
	err   error
}

// OpenWriter opens (or creates) the JSONL file at path, creating parent directories
// as needed, and starts the background write goroutine.
func OpenWriter(path string) (*Writer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	w := &Writer{
		f:    f,
		out:  f,
		ch:   make(chan any, 64),
		done: make(chan struct{}),
	}
	go w.run()
	return w, nil
}

// Emit sends ev to the background writer. It does not block unless the
// channel buffer (64) is full. Events emitted after Close are dropped.
func (w *Writer) Emit(ev any) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		slog.Warn("session: dropping event emitted after close", "type", fmt.Sprintf("%T", ev))
		return
	}
	w.ch <- ev
}

// Err returns the first write, encode or sync error seen by the background
// writer, or nil if none has occurred.
func (w *Writer) Err() error {
	w.errMu.Lock()
	defer w.errMu.Unlock()
	return w.err
}

func (w *Writer) setErr(err error) {
	if err == nil {
		return
	}
	w.errMu.Lock()
	defer w.errMu.Unlock()
	if w.err == nil {
		w.err = err
	}
}

// Close flushes all queued events and closes the file.
// It is idempotent: the second and subsequent calls are no-ops that return nil.
func (w *Writer) Close() error {
	w.closeOnce.Do(func() {
		w.mu.Lock()
		w.closed = true
		close(w.ch)
		w.mu.Unlock()
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
		line, err := encode(ev)
		if err != nil {
			// Skip unencodable events rather than crashing the goroutine.
			w.setErr(err)
			continue
		}
		// One write per line, so a failed write is not followed by a lone
		// newline.
		_, err = w.out.Write(append(line, '\n'))
		w.setErr(err)
		if isCritical(ev) {
			w.setErr(w.f.Sync())
		}
	}
}
