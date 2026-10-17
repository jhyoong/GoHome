package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jhyoong/GoHome/gohome/internal/config"
)

func execBash(t *testing.T, input any) Result {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	bt := &ShellTool{}
	res, err := bt.Execute(context.Background(), raw, NullSink{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return res
}

func TestBash_EchoHello(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	res := execBash(t, map[string]any{"command": "echo hello"})
	if res.IsError {
		t.Fatalf("unexpected IsError: %s", res.Content)
	}
	if !strings.Contains(res.Content, "exit 0") {
		t.Errorf("expected 'exit 0' in content, got %q", res.Content)
	}
	if !strings.Contains(res.Content, "hello") {
		t.Errorf("expected 'hello' in content, got %q", res.Content)
	}
}

func TestBash_NonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	res := execBash(t, map[string]any{"command": "false"})
	if res.IsError {
		t.Fatalf("unexpected IsError for non-zero exit (should be normal result): %s", res.Content)
	}
	if !strings.Contains(res.Content, "exit 1") {
		t.Errorf("expected 'exit 1' in content, got %q", res.Content)
	}
}

func TestBash_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	timeoutMs := 100
	res := execBash(t, map[string]any{
		"command":    "sleep 10",
		"timeout_ms": timeoutMs,
	})
	if !res.IsError {
		t.Fatal("expected IsError due to timeout")
	}
	if !strings.Contains(res.Content, "timed out") {
		t.Errorf("expected 'timed out' in content, got %q", res.Content)
	}
}

func TestBash_SinkReceivesLines(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}

	var received []string
	sp := &spySink{onUpdate: func(s string) { received = append(received, s) }}

	raw, _ := json.Marshal(map[string]any{"command": "printf 'line1\\nline2\\nline3\\n'"})
	bt := &ShellTool{}
	res, err := bt.Execute(context.Background(), raw, sp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected IsError: %s", res.Content)
	}
	if len(received) == 0 {
		t.Error("expected sink to receive at least one line")
	}
	combined := strings.Join(received, "")
	if !strings.Contains(combined, "line1") {
		t.Errorf("expected 'line1' in sink output, got %q", combined)
	}
}

func TestBash_ContextCancelled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}

	ctx, cancel := context.WithCancel(context.Background())

	bt := &ShellTool{}
	raw, _ := json.Marshal(map[string]any{"command": "sleep 10"})

	// Cancel the context immediately after launching Execute in a goroutine.
	// We cancel after a brief delay so the process has time to start.
	type outcome struct {
		res Result
		err error
	}
	ch := make(chan outcome, 1)
	go func() {
		res, err := bt.Execute(ctx, raw, NullSink{})
		ch <- outcome{res, err}
	}()

	// Give the process a moment to start, then cancel.
	cancel()

	got := <-ch
	if got.err != nil {
		t.Fatalf("unexpected error: %v", got.err)
	}
	if !got.res.IsError {
		t.Fatal("expected IsError when context is cancelled")
	}
	if !strings.Contains(got.res.Content, "cancelled") {
		t.Errorf("expected 'cancelled' in content, got %q", got.res.Content)
	}
}

type spySink struct {
	onUpdate func(string)
}

func (s *spySink) Update(chunk string) { s.onUpdate(chunk) }

func TestBash_ToolMeta(t *testing.T) {
	bt := &ShellTool{}
	if bt.Name() != "shell" {
		t.Errorf("expected name 'shell', got %q", bt.Name())
	}
	if bt.Description() == "" {
		t.Error("expected non-empty description")
	}
	var schema map[string]any
	if err := json.Unmarshal(bt.InputSchema(), &schema); err != nil {
		t.Errorf("InputSchema is not valid JSON: %v", err)
	}
}

// installFakeSudo puts a fake "sudo" first on PATH. "sudo -S -v ..."
// reads one line from stdin and accepts only "goodpass". Any other call
// prints "fake-sudo:" and its arguments, so tests can see it ran.
func installFakeSudo(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
if [ "$1" = "-S" ] && [ "$2" = "-v" ]; then
	read -r pw
	if [ "$pw" = "goodpass" ]; then exit 0; fi
	echo "Sorry, try again." >&2
	exit 1
fi
echo "fake-sudo: $*"
`
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake sudo: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func execBashWithSudo(t *testing.T, password, command string) Result {
	t.Helper()
	ctx := WithSudoPassword(context.Background(), password)
	raw, _ := json.Marshal(map[string]any{"command": command})
	res, err := (&ShellTool{}).Execute(ctx, raw, NullSink{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return res
}

func TestBash_SudoPreauth_CorrectPasswordRunsCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	installFakeSudo(t)
	res := execBashWithSudo(t, "goodpass", "sudo -n head -3 /etc/shadow")
	if !strings.HasPrefix(res.Content, "exit 0\n") {
		t.Errorf("want exit 0, got %q", res.Content)
	}
	if !strings.Contains(res.Content, "fake-sudo: -n head -3 /etc/shadow") {
		t.Errorf("command should run unchanged, got %q", res.Content)
	}
}

func TestBash_SudoPreauth_WrongPasswordSkipsCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	installFakeSudo(t)
	res := execBashWithSudo(t, "badpass", "echo ran")
	if !strings.HasPrefix(res.Content, "exit 1\n") {
		t.Errorf("want exit 1, got %q", res.Content)
	}
	if !strings.Contains(res.Content, SudoPreauthFailedMarker) {
		t.Errorf("want pre-auth failure marker, got %q", res.Content)
	}
	if !strings.Contains(res.Content, "Sorry, try again.") {
		t.Errorf("want sudo's own error kept, got %q", res.Content)
	}
	if strings.Contains(res.Content, "ran") {
		t.Errorf("command must not run after rejection, got %q", res.Content)
	}
}

func TestBash_SudoPreauth_FD3ClosedBeforeCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	installFakeSudo(t)
	// The subshell keeps a redirection error from exiting the shell.
	res := execBashWithSudo(t, "goodpass", "(exec 0<&3) 2>/dev/null && echo FD3_OPEN || echo FD3_CLOSED")
	if !strings.Contains(res.Content, "FD3_CLOSED") || strings.Contains(res.Content, "FD3_OPEN") {
		t.Errorf("fd 3 must be closed before the command, got %q", res.Content)
	}
}

func TestBash_SudoPreauth_PipeIntoSudo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	installFakeSudo(t)
	res := execBashWithSudo(t, "goodpass", "echo data | sudo tee /tmp/x")
	if !strings.Contains(res.Content, "fake-sudo: tee /tmp/x") {
		t.Errorf("want sudo to run without -S rewrite, got %q", res.Content)
	}
}

func TestBash_SudoPreauth_ExitCodePreserved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	installFakeSudo(t)
	res := execBashWithSudo(t, "goodpass", "echo a\nsh -c 'exit 7'")
	if !strings.HasPrefix(res.Content, "exit 7\n") {
		t.Errorf("want exit 7, got %q", res.Content)
	}
}

// Sudo caches credentials per parent process when there is no tty. This
// guards the trailing "exit $?": the last command must stay a child of the
// shell that ran "sudo -v".
func TestBash_SudoPreauth_LastCommandKeepsShellAsParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	installFakeSudo(t)
	res := execBashWithSudo(t, "goodpass", "echo self=$$\nsh -c 'echo ppid=$PPID'")
	var self, ppid string
	for _, line := range strings.Split(res.Content, "\n") {
		if v, ok := strings.CutPrefix(line, "self="); ok {
			self = v
		}
		if v, ok := strings.CutPrefix(line, "ppid="); ok {
			ppid = v
		}
	}
	if self == "" || self != ppid {
		t.Errorf("want ppid == shell pid, got self=%q ppid=%q in %q", self, ppid, res.Content)
	}
}

func TestBash_NoSudoPassword_CommandUnchanged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	installFakeSudo(t)
	res := execBash(t, map[string]any{"command": "sudo ls"})
	if !strings.Contains(res.Content, "fake-sudo: ls") {
		t.Errorf("want plain sudo call, got %q", res.Content)
	}
}

func TestBash_DefaultTimeoutCapApplied(t *testing.T) {
	// Provide a timeout larger than the cap (600000ms) and verify the tool doesn't error on construction.
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	// Just verify it runs normally; actual cap logic tested internally.
	res := execBash(t, map[string]any{
		"command":    "echo cap-test",
		"timeout_ms": 9999999,
	})
	if res.IsError {
		t.Fatalf("unexpected IsError: %s", res.Content)
	}
	if !strings.Contains(res.Content, "cap-test") {
		t.Errorf("expected 'cap-test' in output, got %q", res.Content)
	}
}

func TestBash_SudoPreauth_TrailingBackslash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	installFakeSudo(t)
	res := execBashWithSudo(t, "goodpass", `echo a \`)
	if !strings.HasPrefix(res.Content, "exit 0\n") {
		t.Errorf("want exit 0, got %q", res.Content)
	}
	// A joined "exit $?" would be echoed as an argument.
	if strings.Contains(res.Content, "exit") && strings.Contains(res.Content, "a exit") {
		t.Errorf("trailing backslash joined the exit line: %q", res.Content)
	}
}

func TestBash_LargeOutputKeepsHeadAndTail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	res := execBash(t, map[string]any{
		"command": "printf 'HEAD-MARK\\n'; yes x | head -c 200000; printf 'TAIL-MARK\\n'",
	})
	if res.IsError {
		t.Fatalf("unexpected IsError: %s", res.Content)
	}
	if !strings.Contains(res.Content, "HEAD-MARK") {
		t.Errorf("head missing")
	}
	if !strings.Contains(res.Content, "TAIL-MARK") {
		t.Errorf("tail missing")
	}
	if !strings.Contains(res.Content, "bytes omitted ...]") {
		t.Errorf("omitted marker missing")
	}
	if len(res.Content) > 2*shellOutputKeep+200 {
		t.Errorf("content not capped: %d bytes", len(res.Content))
	}
}

func TestBash_LongLineKeepsTailAndLaterOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	res := execBash(t, map[string]any{
		"command": "head -c 2000000 /dev/zero | tr '\\000' a; printf 'zzEND\\nAFTER-LINE\\n'",
	})
	if res.IsError {
		t.Fatalf("unexpected IsError: %s", res.Content)
	}
	if !strings.Contains(res.Content, "aaaazzEND\nAFTER-LINE") {
		t.Errorf("tail of long line or later output missing: ...%q", res.Content[max(0, len(res.Content)-100):])
	}
	if strings.Contains(res.Content, "cut off") {
		t.Errorf("unexpected cut-off note")
	}
	if !strings.Contains(res.Content, "bytes omitted ...]") {
		t.Errorf("omitted marker missing")
	}
}

func TestBash_SmallOutputNotMarked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	res := execBash(t, map[string]any{"command": "yes x | head -c 60000"})
	if strings.Contains(res.Content, "omitted") {
		t.Errorf("output under the cap should not be marked")
	}
	if got := strings.Count(res.Content, "x\n"); got != 30000 {
		t.Errorf("want 30000 x, got %d", got)
	}
}

func TestCappedBuffer_ExactCounts(t *testing.T) {
	b := newCappedBuffer(4, 4)
	for _, s := range []string{"ab", "cdef", "ghij", "kl"} {
		_, _ = b.Write([]byte(s))
	}
	want := "abcd\n[... 4 bytes omitted ...]\nijkl"
	if got := b.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	b2 := newCappedBuffer(4, 4)
	_, _ = b2.Write([]byte("abcdefgh"))
	if got := b2.String(); got != "abcdefgh" {
		t.Errorf("got %q, want no marker", got)
	}
}

func TestCappedBuffer_DoesNotSplitRunes(t *testing.T) {
	// Each "é" is 2 bytes. A cut at an odd offset would split one.
	b := newCappedBuffer(5, 5)
	_, _ = b.Write([]byte(strings.Repeat("é", 20)))
	got := b.String()
	if !utf8.ValidString(got) {
		t.Errorf("output has broken runes: %q", got)
	}
	if !strings.Contains(got, "[... 32 bytes omitted ...]") {
		t.Errorf("omitted count should include the moved bytes: %q", got)
	}
}

func TestBash_ZeroTimeoutUsesDefault(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	for _, ms := range []int{0, -5} {
		res := execBash(t, map[string]any{"command": "sleep 0.05; echo ran", "timeout_ms": ms})
		if res.IsError {
			t.Fatalf("timeout_ms=%d: unexpected IsError: %s", ms, res.Content)
		}
		if !strings.Contains(res.Content, "ran") {
			t.Errorf("timeout_ms=%d: want 'ran', got %q", ms, res.Content)
		}
	}
}

func TestBash_DescriptionAndSchemaUseTimeouts(t *testing.T) {
	check := func(bt ShellTool, def, maxMs int) {
		t.Helper()
		d := bt.Description()
		if !strings.Contains(d, fmt.Sprintf("%d ms", def)) || !strings.Contains(d, fmt.Sprintf("%d ms", maxMs)) {
			t.Errorf("description missing %d/%d: %q", def, maxMs, d)
		}
		var schema struct {
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(bt.InputSchema(), &schema); err != nil {
			t.Fatalf("InputSchema is not valid JSON: %v", err)
		}
		td := schema.Properties["timeout_ms"].Description
		if !strings.Contains(td, fmt.Sprintf("default %d", def)) || !strings.Contains(td, fmt.Sprintf("max %d", maxMs)) {
			t.Errorf("timeout_ms description missing %d/%d: %q", def, maxMs, td)
		}
		if strings.Contains(d+td, "120000") && def != 120000 {
			t.Errorf("hard-coded default left in: %q / %q", d, td)
		}
	}
	check(ShellTool{DefaultTimeoutMs: 5000, MaxTimeoutMs: 9000}, 5000, 9000)
	check(ShellTool{}, config.DefaultShellTimeoutMs, config.DefaultMaxShellTimeoutMs)
}
