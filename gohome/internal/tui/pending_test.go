package tui

import (
	"strings"
	"testing"
)

func TestPendingMessages_RenderEmpty(t *testing.T) {
	msgs := []string{}
	c := NewPendingMessages(&msgs)
	lines := c.Render(80)
	if len(lines) != 0 {
		t.Errorf("empty queue should render 0 lines, got %d", len(lines))
	}
}

func TestPendingMessages_RenderWithMessages(t *testing.T) {
	msgs := []string{"fix the tests", "update the README"}
	c := NewPendingMessages(&msgs)
	lines := c.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "Queued:") {
		t.Errorf("header missing: %q", joined)
	}
	if !strings.Contains(joined, "[1]") {
		t.Errorf("[1] marker missing: %q", joined)
	}
	if !strings.Contains(joined, "fix the tests") {
		t.Errorf("first message missing: %q", joined)
	}
	if !strings.Contains(joined, "[2]") {
		t.Errorf("[2] marker missing: %q", joined)
	}
	if !strings.Contains(joined, "update the README") {
		t.Errorf("second message missing: %q", joined)
	}
}

func TestPendingMessages_TruncatesLongMessages(t *testing.T) {
	long := strings.Repeat("x", 200)
	msgs := []string{long}
	c := NewPendingMessages(&msgs)
	lines := c.Render(80)
	for _, l := range lines {
		if VisualWidth(StripAnsi(l)) > 80 {
			t.Errorf("line exceeds width: %d cols", VisualWidth(StripAnsi(l)))
		}
	}
}
