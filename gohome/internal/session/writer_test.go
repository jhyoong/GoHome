package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
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

	w.Emit(SessionStart{ID: "s1", CWD: "/tmp", Model: "m", Endpoint: "e"})
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
