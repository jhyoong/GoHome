package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestMouseWheelSchedulesOneIdleTimer(t *testing.T) {
	m := New(nil, "main")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	seq := m.mouseIdleSeq
	if !m.mouseIdlePending {
		t.Fatal("expected an idle timer pending after the first wheel event")
	}
	for range 5 {
		m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	}
	if m.mouseIdleSeq != seq {
		t.Errorf("expected no new timers while one is pending: seq %d -> %d", seq, m.mouseIdleSeq)
	}
}

func TestMouseIdleReschedulesAfterRecentWheel(t *testing.T) {
	m := New(nil, "main")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})

	// The timer fires while the last wheel event is still recent.
	_, cmd := m.Update(mouseIdleMsg{seq: m.mouseIdleSeq})
	if !m.mouseActive {
		t.Fatal("mouse capture must stay on when the wheel was used less than 2s ago")
	}
	if cmd == nil || !m.mouseIdlePending {
		t.Fatal("expected the idle timer to be rescheduled")
	}

	// The rescheduled timer fires after 2s of no wheel activity.
	m.lastWheel = time.Now().Add(-3 * time.Second)
	_, cmd = m.Update(mouseIdleMsg{seq: m.mouseIdleSeq})
	if m.mouseActive {
		t.Error("expected mouse capture off after 2s of no wheel activity")
	}
	if cmd == nil {
		t.Error("expected a command to disable mouse capture")
	}
	if m.mouseIdlePending {
		t.Error("expected no timer pending after the idle timeout")
	}

	// The next wheel event starts a new timer.
	seq := m.mouseIdleSeq
	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	if !m.mouseActive || !m.mouseIdlePending || m.mouseIdleSeq == seq {
		t.Error("expected the next wheel event to re-enable capture and start a new timer")
	}
}
