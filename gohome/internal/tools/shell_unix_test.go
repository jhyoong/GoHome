//go:build !windows

package tools

import (
	"strings"
	"testing"
	"time"
)

// execBashGuarded runs execBash but fails instead of hanging if it does not
// return within guard.
func execBashGuarded(t *testing.T, guard time.Duration, input any) (Result, time.Duration) {
	t.Helper()
	type out struct {
		res Result
		dur time.Duration
	}
	ch := make(chan out, 1)
	go func() {
		start := time.Now()
		r := execBash(t, input)
		ch <- out{r, time.Since(start)}
	}()
	select {
	case o := <-ch:
		return o.res, o.dur
	case <-time.After(guard):
		t.Fatalf("shell did not return within %v", guard)
		return Result{}, 0
	}
}

func TestBash_HugeSingleLineDoesNotHang(t *testing.T) {
	res, dur := execBashGuarded(t, 20*time.Second, map[string]any{
		"command":    "head -c 2000000 /dev/zero | tr '\\0' a; echo; echo after",
		"timeout_ms": 5000,
	})
	if dur > 5*time.Second {
		t.Errorf("took too long: %v", dur)
	}
	if !strings.Contains(res.Content, "truncated") && !strings.Contains(res.Content, "cut off") {
		t.Errorf("expected cut-off note, got %.200q", res.Content)
	}
}

func TestBash_TimeoutKeepsPartialOutput(t *testing.T) {
	res, _ := execBashGuarded(t, 20*time.Second, map[string]any{
		"command":    "echo partial-out; sleep 10",
		"timeout_ms": 500,
	})
	if !res.IsError || !strings.Contains(res.Content, "timed out") {
		t.Fatalf("expected timeout error, got %q", res.Content)
	}
	if !strings.Contains(res.Content, "partial-out") {
		t.Errorf("expected partial output kept, got %q", res.Content)
	}
}

func TestBash_TimeoutKillsChildProcess(t *testing.T) {
	res, dur := execBashGuarded(t, 20*time.Second, map[string]any{
		"command":    "sleep 4; echo done",
		"timeout_ms": 300,
	})
	if !res.IsError || !strings.Contains(res.Content, "timed out") {
		t.Fatalf("expected timeout error, got %q", res.Content)
	}
	if dur > 2*time.Second {
		t.Errorf("expected return near timeout, took %v", dur)
	}
}

func TestBash_TimeoutWithBackgroundJob(t *testing.T) {
	res, dur := execBashGuarded(t, 40*time.Second, map[string]any{
		"command":    "sleep 30 & echo bg; sleep 30",
		"timeout_ms": 300,
	})
	if !res.IsError || !strings.Contains(res.Content, "timed out") {
		t.Fatalf("expected timeout error, got %q", res.Content)
	}
	if dur > 3*time.Second {
		t.Errorf("expected return near timeout, took %v", dur)
	}
}

// TestBash_RunsWithoutControllingTerminal checks that the shell starts in its
// own session: it leads its own process group (Setsid implies this, while an
// inherited group would not) and cannot open /dev/tty, so programs like sudo
// cannot prompt over the TUI.
func TestBash_RunsWithoutControllingTerminal(t *testing.T) {
	res := execBash(t, map[string]any{
		"command": `[ "$(ps -o pgid= -p $$ | tr -d ' ')" = "$$" ] && echo GROUP_LEADER; ( : < /dev/tty ) 2>/dev/null || echo NO_TTY`,
	})
	if res.IsError {
		t.Fatalf("unexpected IsError: %s", res.Content)
	}
	for _, want := range []string{"GROUP_LEADER", "NO_TTY"} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("expected %q in output, got %q", want, res.Content)
		}
	}
}
