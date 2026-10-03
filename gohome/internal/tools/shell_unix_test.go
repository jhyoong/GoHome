//go:build !windows

package tools

import (
	"strings"
	"testing"
)

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
