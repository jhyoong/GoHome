package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

func sampleListings() []session.Listing {
	now := time.Now()
	return []session.Listing{
		{
			ID:         "abc123",
			Title:      "fix the login bug",
			StartedAt:  now.Add(-2 * time.Hour),
			LastActive: now.Add(-1 * time.Hour),
			Path:       "/tmp/test-abc123.jsonl",
		},
		{
			ID:         "def456",
			Title:      "",
			StartedAt:  now.Add(-48 * time.Hour),
			LastActive: now.Add(-24 * time.Hour),
			Path:       "/tmp/test-def456.jsonl",
		},
		{
			ID:         "ghi789",
			Title:      "refactor the TUI renderer",
			StartedAt:  now.Add(-168 * time.Hour),
			LastActive: now.Add(-72 * time.Hour),
			Path:       "/tmp/test-ghi789.jsonl",
		},
	}
}

func TestSessionBrowserRender(t *testing.T) {
	sb := NewSessionBrowser(sampleListings())
	lines := sb.Render(80)
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines (search + 3 items), got %d", len(lines))
	}
	joined := strings.Join(lines, "\n")
	plainJoined := StripAnsi(joined)
	if !strings.Contains(plainJoined, "fix the login bug") {
		t.Error("should show session title")
	}
}

func TestSessionBrowserUsesIDWhenNoTitle(t *testing.T) {
	sb := NewSessionBrowser(sampleListings())
	lines := sb.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "def456") {
		t.Error("should fall back to session ID when title is empty")
	}
}

func TestSessionBrowserSelectReturnsID(t *testing.T) {
	var selectedID string
	sb := NewSessionBrowser(sampleListings())
	sb.SetOnSelect(func(id string) { selectedID = id })
	sb.list.HandleInput(tea.KeyMsg{Type: tea.KeyDown})
	sb.list.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})
	if selectedID != "def456" {
		t.Errorf("expected 'def456', got %q", selectedID)
	}
}

func TestSessionBrowserCancel(t *testing.T) {
	cancelled := false
	sb := NewSessionBrowser(sampleListings())
	sb.SetOnCancel(func() { cancelled = true })
	sb.list.HandleInput(tea.KeyMsg{Type: tea.KeyEsc})
	if !cancelled {
		t.Error("Escape should call onCancel")
	}
}

func TestSessionBrowserRelativeTime(t *testing.T) {
	sb := NewSessionBrowser(sampleListings())
	lines := sb.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "ago") {
		t.Errorf("should show relative time: %s", joined)
	}
}

func TestRelativeTime(t *testing.T) {
	tests := []struct {
		dur    time.Duration
		expect string
	}{
		{30 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{2 * time.Hour, "2h ago"},
		{48 * time.Hour, "2d ago"},
		{14 * 24 * time.Hour, "2w ago"},
	}
	now := time.Now()
	for _, tt := range tests {
		got := relativeTime(now.Add(-tt.dur))
		if got != tt.expect {
			t.Errorf("relativeTime(%v) = %q, want %q", tt.dur, got, tt.expect)
		}
	}
}
