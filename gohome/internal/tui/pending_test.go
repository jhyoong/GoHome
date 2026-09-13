package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
)

func windowSizeMsg(w, h int) tea.WindowSizeMsg {
	return tea.WindowSizeMsg{Width: w, Height: h}
}

func ctrlDMsg() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyCtrlD}
}

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

func TestPendingMessages_DrainCombines(t *testing.T) {
	m := New(nil, "")
	m.Update(windowSizeMsg(80, 24))

	sv := m.Sessions()["main"]
	sv.InFlight = true
	m.SetPendingMessages([]string{"fix tests", "update docs", "run lint"})

	m.Update(agentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventRunDone,
		SessionID: "main",
	}})

	tl := sv.Timeline
	if len(tl) == 0 {
		t.Fatal("expected a KindUser entry after drain")
	}
	last := tl[len(tl)-1]
	if last.Kind != KindUser {
		t.Fatalf("expected KindUser, got %s", last.Kind)
	}
	if last.Text != "fix tests\nupdate docs\nrun lint" {
		t.Errorf("expected combined text, got %q", last.Text)
	}
	if len(m.GetPendingMessages()) != 0 {
		t.Errorf("pending queue should be empty, got %d", len(m.GetPendingMessages()))
	}
}

func TestPendingMessages_CtrlD_PopsLast(t *testing.T) {
	m := New(nil, "")
	m.Update(windowSizeMsg(80, 24))

	m.SetPendingMessages([]string{"msg1", "msg2", "msg3"})

	m.Update(ctrlDMsg())

	got := m.GetPendingMessages()
	if len(got) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(got))
	}
	if got[1] != "msg2" {
		t.Errorf("expected msg2 as last, got %q", got[1])
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
