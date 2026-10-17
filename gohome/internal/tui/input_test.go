package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// startRun submits a message so the main run is in flight.
func startRun(t *testing.T) *Model {
	t.Helper()
	m := New(nil, "main")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	typeRunes(m, "go")
	pressKey(m, tea.KeyEnter)
	if !m.runActive() {
		t.Fatal("setup: run not active")
	}
	return m
}

func TestEscClosesFilePopupWithoutCancel(t *testing.T) {
	m := startRun(t)
	m.fileSearching = true
	m.fileSearch.visible = true

	pressKey(m, tea.KeyEsc)

	if m.fileSearch.visible || m.fileSearching {
		t.Error("popup still open after Esc")
	}
	if !m.mainSession().InFlight {
		t.Error("Esc on the popup cancelled the run")
	}
	if !m.lastEsc.IsZero() {
		t.Error("popup Esc counted as the first press")
	}
}

func TestDoubleEscCancelsRun(t *testing.T) {
	m := startRun(t)

	pressKey(m, tea.KeyEsc)
	if !m.mainSession().InFlight {
		t.Fatal("single Esc cancelled the run")
	}
	if m.statusMsg != "Press Esc again to cancel" {
		t.Errorf("statusMsg = %q", m.statusMsg)
	}

	pressKey(m, tea.KeyEsc)
	if m.mainSession().InFlight {
		t.Error("second Esc within the window did not cancel")
	}
}

func TestSlowEscPressesDoNotCancel(t *testing.T) {
	m := startRun(t)

	pressKey(m, tea.KeyEsc)
	m.lastEsc = time.Now().Add(-600 * time.Millisecond)
	pressKey(m, tea.KeyEsc)

	if !m.mainSession().InFlight {
		t.Error("Esc presses 600ms apart cancelled the run")
	}
	if m.statusMsg != "Press Esc again to cancel" {
		t.Errorf("statusMsg = %q", m.statusMsg)
	}
}

func TestSpinnerHintMentionsDoubleEsc(t *testing.T) {
	s := NewSpinner()
	s.Start("Working...")
	s.SetOnCancel(func() {})
	if got := StripAnsi(s.Render(80)[0]); !strings.Contains(got, "(Esc Esc to cancel)") {
		t.Errorf("hint missing in %q", got)
	}
}

func TestUnknownSlashInputIsSentAsMessage(t *testing.T) {
	m := New(nil, "main")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	typeRunes(m, "/usr/bin/python3 crashes, why?")
	pressKey(m, tea.KeyEnter)

	if !m.mainSession().InFlight {
		t.Fatal("message was not sent")
	}
	tl := m.mainSession().Timeline
	if last := tl[len(tl)-1]; last.Kind != KindUser || last.Text != "/usr/bin/python3 crashes, why?" {
		t.Errorf("last entry = %+v", last)
	}
}

func TestDownToLastEntryResumesAutoScroll(t *testing.T) {
	m := New(nil, "main")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for i := 0; i < 5; i++ {
		m.AddTimelineEntry("main", TimelineEntry{Kind: KindNotice, Text: "n"})
	}
	m.cursor = 4
	pressKey(m, tea.KeyUp)
	pressKey(m, tea.KeyUp)
	m.chat.DisableAutoScroll(m.winW)
	pressKey(m, tea.KeyDown)
	pressKey(m, tea.KeyDown)

	if m.cursor != len(m.mainSession().Timeline)-1 {
		t.Fatalf("cursor = %d, want last entry", m.cursor)
	}
	if !m.chat.autoScroll {
		t.Error("auto-scroll not re-enabled at the last entry")
	}
}

func TestPromptHistoryRecall(t *testing.T) {
	m := New(nil, "main")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, s := range []string{"first", "second"} {
		typeRunes(m, s)
		pressKey(m, tea.KeyEnter)
		m.mainSession().InFlight = false
		m.mainRuns = 0
	}

	m.editor.HandleInput(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.editor.Value(); got != "second" {
		t.Errorf("first Up = %q, want second", got)
	}
	m.editor.HandleInput(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.editor.Value(); got != "first" {
		t.Errorf("second Up = %q, want first", got)
	}
}

func TestUpOnDraftKeepsCursor(t *testing.T) {
	e := NewEditor(80, 24)
	e.history.Add("old")
	e.SetValue("draft")
	line, col := e.cursorLine, e.cursorCol

	e.HandleInput(tea.KeyMsg{Type: tea.KeyUp})

	if e.Value() != "draft" || e.cursorLine != line || e.cursorCol != col {
		t.Errorf("value=%q cursor=(%d,%d), want draft at (%d,%d)", e.Value(), e.cursorLine, e.cursorCol, line, col)
	}
}

func TestHistoryPrevWithoutBrowsingReturnsDraft(t *testing.T) {
	h := NewHistory(10)
	h.Add("a")
	h.draft = "d"
	if got := h.Prev(); got != "d" {
		t.Errorf("Prev = %q, want draft", got)
	}
}

func TestCancelWhenIdle(t *testing.T) {
	m := New(nil, "main")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	before := len(m.mainSession().Timeline)
	typeRunes(m, "/cancel")
	pressKey(m, tea.KeyEnter)

	if m.statusMsg != "Nothing to cancel" {
		t.Errorf("statusMsg = %q", m.statusMsg)
	}
	if n := len(m.mainSession().Timeline); n != before {
		t.Errorf("timeline grew from %d to %d", before, n)
	}
}
