package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func mustTempFile(t *testing.T, lines int) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "read_test_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= lines; i++ {
		_, _ = fmt.Fprintf(f, "line %d\n", i)
	}
	_ = f.Close()
	return f.Name()
}

func execRead(t *testing.T, input any) Result {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	rt := &ReadTool{}
	res, err := rt.Execute(context.Background(), raw, NullSink{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return res
}

func TestRead_BasicLineNumbers(t *testing.T) {
	path := mustTempFile(t, 3)
	res := execRead(t, map[string]any{"path": path})

	if res.IsError {
		t.Fatalf("unexpected error result: %s", res.Content)
	}
	lines := strings.Split(strings.TrimRight(res.Content, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 output lines, got %d:\n%s", len(lines), res.Content)
	}
	// Each line should be "<lineno>\t<content>"
	for i, l := range lines {
		want := fmt.Sprintf("%d\tline %d", i+1, i+1)
		if !strings.HasPrefix(l, want) {
			t.Errorf("line %d: got %q, want prefix %q", i+1, l, want)
		}
	}
}

func TestRead_OffsetAndLimit(t *testing.T) {
	path := mustTempFile(t, 10)
	res := execRead(t, map[string]any{"path": path, "offset": 3, "limit": 4})

	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	lines := strings.Split(strings.TrimRight(res.Content, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d", len(lines))
	}
	// First returned line should have lineno 3
	if !strings.HasPrefix(lines[0], "3\t") {
		t.Errorf("expected first line to be lineno 3, got %q", lines[0])
	}
	// Last returned line should have lineno 6
	if !strings.HasPrefix(lines[3], "6\t") {
		t.Errorf("expected last line to be lineno 6, got %q", lines[3])
	}
}

func TestRead_MissingFile(t *testing.T) {
	res := execRead(t, map[string]any{"path": "/nonexistent/definitely/missing.txt"})
	if !res.IsError {
		t.Fatal("expected IsError for missing file")
	}
	if res.Content == "" {
		t.Error("expected non-empty error message")
	}
}

func TestRead_Default2000Cap(t *testing.T) {
	path := mustTempFile(t, 2500)
	res := execRead(t, map[string]any{"path": path})

	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	lines := strings.Split(strings.TrimRight(res.Content, "\n"), "\n")
	if len(lines) != 2000 {
		t.Fatalf("expected 2000 lines (default cap), got %d", len(lines))
	}
}

func TestRead_ToolMeta(t *testing.T) {
	rt := &ReadTool{}
	if rt.Name() != "read" {
		t.Errorf("expected name 'read', got %q", rt.Name())
	}
	if rt.Description() == "" {
		t.Error("expected non-empty description")
	}
	var schema map[string]any
	if err := json.Unmarshal(rt.InputSchema(), &schema); err != nil {
		t.Errorf("InputSchema is not valid JSON: %v", err)
	}
}

func TestRead_RelativePath(t *testing.T) {
	// Write a temp file in the temp dir, then verify we can reach it via absolute path.
	dir := t.TempDir()
	path := filepath.Join(dir, "rel.txt")
	_ = os.WriteFile(path, []byte("hello\n"), 0644)

	res := execRead(t, map[string]any{"path": path})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "hello") {
		t.Errorf("expected 'hello' in output, got %q", res.Content)
	}
}

func TestRead_HugeLineTruncated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge.txt")
	long := strings.Repeat("a", 2*1024*1024)
	if err := os.WriteFile(path, []byte("first\n"+long+"\nlast\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res := execRead(t, map[string]any{"path": path})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	lines := strings.Split(strings.TrimRight(res.Content, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	want := "2\t" + strings.Repeat("a", maxReadLineRunes) + readLineTruncatedMarker
	if lines[1] != want {
		t.Errorf("line 2 not truncated as expected: len=%d", len(lines[1]))
	}
	if lines[2] != "3\tlast" {
		t.Errorf("line 3: got %q", lines[2])
	}
}

func TestRead_MultiByteLineCutOnRuneBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mb.txt")
	long := strings.Repeat("é世", maxReadLineRunes) // 2*max runes
	if err := os.WriteFile(path, []byte(long+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res := execRead(t, map[string]any{"path": path})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	line := strings.TrimSuffix(res.Content, "\n")
	body := strings.TrimPrefix(line, "1\t")
	if !strings.HasSuffix(body, readLineTruncatedMarker) {
		t.Fatalf("missing marker: %q", body[len(body)-40:])
	}
	body = strings.TrimSuffix(body, readLineTruncatedMarker)
	if !utf8.ValidString(body) {
		t.Fatal("truncated line is not valid UTF-8")
	}
	if n := utf8.RuneCountInString(body); n != maxReadLineRunes {
		t.Errorf("expected %d runes, got %d", maxReadLineRunes, n)
	}
}

func TestRead_OutputByteCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.txt")
	var b strings.Builder
	line := strings.Repeat("x", 1000)
	for i := 0; i < 500; i++ {
		b.WriteString(line)
		b.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	res := execRead(t, map[string]any{"path": path})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if len(res.Content) > maxReadOutputBytes+200 {
		t.Errorf("output too large: %d bytes", len(res.Content))
	}
	lines := strings.Split(strings.TrimRight(res.Content, "\n"), "\n")
	note := lines[len(lines)-1]
	lastLine := lines[len(lines)-2]
	var n int
	if _, err := fmt.Sscanf(lastLine, "%d\t", &n); err != nil {
		t.Fatalf("cannot parse last line number from %q: %v", lastLine[:20], err)
	}
	want := fmt.Sprintf("[output truncated at line %d; use offset=%d to continue]", n, n+1)
	if note != want {
		t.Errorf("note: got %q, want %q", note, want)
	}
	if n >= 500 || n < 50 {
		t.Errorf("unexpected last line %d", n)
	}

	// Continuing from the suggested offset starts at the next line.
	res2 := execRead(t, map[string]any{"path": path, "offset": n + 1})
	if !strings.HasPrefix(res2.Content, fmt.Sprintf("%d\t", n+1)) {
		t.Errorf("continuation did not start at line %d", n+1)
	}
}

func TestRead_NoTrailingNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonl.txt")
	if err := os.WriteFile(path, []byte("one\r\ntwo\nthree"), 0644); err != nil {
		t.Fatal(err)
	}
	res := execRead(t, map[string]any{"path": path})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	want := "1\tone\n2\ttwo\n3\tthree\n"
	if res.Content != want {
		t.Errorf("got %q, want %q", res.Content, want)
	}
}
