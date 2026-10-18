package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

func TestWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	w, err := OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}

	w.Emit(SessionStart{ID: "s1", CWD: "/tmp", Model: "m", ModelConfig: "e"})
	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "hello"}}})
	w.Emit(SessionEnd{Reason: "done"})

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open file: %v", err)
	}
	defer func() { _ = f.Close() }()

	wantTypes := []string{"session_start", "user_message", "session_end"}
	scanner := bufio.NewScanner(f)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}

	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	for i, line := range lines {
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("line %d: json.Unmarshal: %v\nraw: %s", i, err, line)
			continue
		}
		var gotType string
		if err := json.Unmarshal(m["type"], &gotType); err != nil {
			t.Errorf("line %d: type unmarshal: %v", i, err)
			continue
		}
		if gotType != wantTypes[i] {
			t.Errorf("line %d: type = %q, want %q", i, gotType, wantTypes[i])
		}
	}
}

func TestWriterCloseIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "idempotent.jsonl")

	w, err := OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}

	w.Emit(SessionEnd{Reason: "test"})

	// First close should succeed.
	if err := w.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	// Second close must not panic and must return nil.
	if err := w.Close(); err != nil {
		t.Errorf("second Close: got %v, want nil", err)
	}
}

func TestWriterMkdirAll(t *testing.T) {
	dir := t.TempDir()
	// Use a nested directory that doesn't exist yet
	path := filepath.Join(dir, "nested", "deep", "session.jsonl")
	w, err := OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter with nested dirs: %v", err)
	}
	w.Emit(SessionEnd{Reason: "test"})
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestOpenWriter_OwnerOnlyPerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits not meaningful on Windows")
	}
	dir := filepath.Join(t.TempDir(), "sessions", "proj")
	path := filepath.Join(dir, "s.jsonl")
	w, err := OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	defer func() { _ = w.Close() }()
	st, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Mode().Perm(); got != 0o700 {
		t.Errorf("dir mode = %o, want 700", got)
	}
	st, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %o, want 600", got)
	}
}

// failWriter fails every write and records what it was given.
type failWriter struct {
	mu    sync.Mutex
	calls [][]byte
}

func (f *failWriter) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]byte(nil), p...))
	return 0, errors.New("disk full")
}

func TestWriterErrReportsWriteFailure(t *testing.T) {
	w, err := OpenWriter(filepath.Join(t.TempDir(), "test.jsonl"))
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if w.Err() != nil {
		t.Fatalf("Err before failure = %v, want nil", w.Err())
	}
	// Set before the first Emit, so the write goroutine sees it.
	w.out = &failWriter{}
	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}})
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if w.Err() == nil {
		t.Fatal("Err() = nil after failed write, want error")
	}
}

// TestWriterNoNewlineAfterFailedWrite checks that each event is written in
// one call that includes the newline, so a failed line write is not followed
// by a lone newline.
func TestWriterNoNewlineAfterFailedWrite(t *testing.T) {
	w, err := OpenWriter(filepath.Join(t.TempDir(), "test.jsonl"))
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	fw := &failWriter{}
	w.out = fw
	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "a"}}})
	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "b"}}})
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if len(fw.calls) != 2 {
		t.Fatalf("write calls = %d, want 2 (one per event): %q", len(fw.calls), fw.calls)
	}
	for i, c := range fw.calls {
		if len(c) < 2 || c[0] != '{' || c[len(c)-1] != '\n' {
			t.Errorf("call %d = %q, want a full JSON line ending in newline", i, c)
		}
	}
}

// TestWriterEmitAfterCloseWarns checks that a dropped late event is logged at
// Warn level, so it is visible without debug logging.
func TestWriterEmitAfterCloseWarns(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	w, err := OpenWriter(filepath.Join(t.TempDir(), "test.jsonl"))
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	w.Emit(SessionEnd{Reason: "late"})
	if out := buf.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "after close") {
		t.Errorf("log = %q, want a WARN about the dropped event", out)
	}
}

func TestWriterEmitAfterCloseDoesNotPanic(t *testing.T) {
	w, err := OpenWriter(filepath.Join(t.TempDir(), "test.jsonl"))
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	w.Emit(SessionEnd{Reason: "late"})
}
