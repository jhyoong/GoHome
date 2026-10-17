package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

// sendEvent delivers an agent event for sessionID through Model.Update.
func sendEvent(m *Model, sessionID string, kind agent.EventKind) {
	m.Update(agentEventMsg{SessionID: sessionID, Ev: agent.Event{Kind: kind, SessionID: sessionID}})
}

// startRunWithChild submits a message on the main session and starts a
// subagent child linked to it. It returns the model and the child ID.
func startRunWithChild(t *testing.T) (*Model, string) {
	t.Helper()
	m := New(nil, "main")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	typeRunes(m, "do the task")
	pressKey(m, tea.KeyEnter)

	m.Update(agentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind: agent.EventToolCallDone, SessionID: "main",
		ToolName: "subagent", ToolCallID: "t1", InputJSON: `{"task":"sub"}`,
	}})
	const childID = "main-sub-1"
	sendEvent(m, childID, agent.EventSessionStarted)
	sendEvent(m, childID, agent.EventSending)
	if m.childToParent[childID] != "main" {
		t.Fatalf("setup: child not linked to main")
	}
	return m, childID
}

// focusSession moves focus to id with Ctrl+Right.
func focusSession(t *testing.T, m *Model, id string) {
	t.Helper()
	for i := 0; i < len(m.order) && m.focused != id; i++ {
		pressKey(m, tea.KeyCtrlRight)
	}
	if m.focused != id {
		t.Fatalf("focused = %q, want %q", m.focused, id)
	}
}

// TestEnterInChildViewIsRefused verifies that a subagent view is read-only:
// Enter shows a status message, keeps the editor text and sends nothing.
func TestEnterInChildViewIsRefused(t *testing.T) {
	m, childID := startRunWithChild(t)
	focusSession(t, m, childID)
	before := len(m.sessions[childID].Timeline)

	typeRunes(m, "hello child")
	pressKey(m, tea.KeyEnter)

	if m.statusMsg != "switch to the main session to send" {
		t.Errorf("statusMsg = %q", m.statusMsg)
	}
	if got := m.editor.Value(); got != "hello child" {
		t.Errorf("editor = %q, want text kept", got)
	}
	if n := len(m.sessions[childID].Timeline); n != before {
		t.Errorf("child timeline grew from %d to %d", before, n)
	}
	if len(m.pendingMessages) != 0 {
		t.Errorf("pending = %v, want empty", m.pendingMessages)
	}
	select {
	case got := <-m.inputCh:
		t.Errorf("input sent from child view: %q", got)
	default:
	}
}

// TestCancelWithChildFocused verifies that cancelling from a subagent view
// cancels the main run: the main session and its child stop being in flight
// and "Cancelled." is added to the main session.
func TestCancelWithChildFocused(t *testing.T) {
	m, childID := startRunWithChild(t)
	var cancelled []string
	m.SetSlashCallbacks(SlashCallbacks{CancelSession: func(id string) { cancelled = append(cancelled, id) }})
	focusSession(t, m, childID)

	pressKey(m, tea.KeyCtrlC)

	main, child := m.sessions["main"], m.sessions[childID]
	if main.InFlight || child.InFlight {
		t.Errorf("InFlight: main=%v child=%v, want both false", main.InFlight, child.InFlight)
	}
	if len(cancelled) != 1 || cancelled[0] != "main" {
		t.Errorf("CancelSession calls = %v, want [main]", cancelled)
	}
	if last := main.Timeline[len(main.Timeline)-1]; last.Kind != KindNotice || last.Text != "Cancelled." {
		t.Errorf("main last entry = %+v, want Cancelled. notice", last)
	}
	for _, e := range child.Timeline {
		if e.Text == "Cancelled." {
			t.Error("Cancelled. notice added to the child session")
		}
	}
	if m.spinner.Active() {
		t.Error("spinner still active after cancel")
	}
}

// TestQueueFollowsMainRun verifies that a child's RunDone and SessionEnded do
// not drain the queue or stop the spinner, and that the main RunDone drains
// the queue into the main session while a child view is focused.
func TestQueueFollowsMainRun(t *testing.T) {
	m, childID := startRunWithChild(t)
	typeRunes(m, "queued msg")
	pressKey(m, tea.KeyEnter)
	if len(m.pendingMessages) != 1 {
		t.Fatalf("setup: pending = %v", m.pendingMessages)
	}
	focusSession(t, m, childID)

	sendEvent(m, childID, agent.EventRunDone)
	m.Update(agentEventMsg{SessionID: childID, Ev: agent.Event{
		Kind: agent.EventSessionEnded, SessionID: childID, EndReason: "done",
	}})

	if len(m.pendingMessages) != 1 {
		t.Errorf("child RunDone drained the queue: pending = %v", m.pendingMessages)
	}
	if !m.spinner.Active() {
		t.Error("child RunDone stopped the spinner")
	}
	child := m.sessions[childID]
	for _, e := range child.Timeline {
		if e.Kind == KindUser && e.Text == "queued msg" {
			t.Error("queued message added to the child session")
		}
	}

	sendEvent(m, "main", agent.EventRunDone)

	if len(m.pendingMessages) != 0 {
		t.Errorf("main RunDone did not drain the queue: pending = %v", m.pendingMessages)
	}
	main := m.sessions["main"]
	if last := main.Timeline[len(main.Timeline)-1]; last.Kind != KindUser || last.Text != "queued msg" {
		t.Errorf("main last entry = %+v, want queued user message", last)
	}
	if !main.InFlight {
		t.Error("main not in flight after the queue was sent")
	}
	if !m.spinner.Active() {
		t.Error("spinner stopped while the queued run is starting")
	}
	select {
	case got := <-m.inputCh:
		t.Errorf("queued input sent before its cmd ran: %q", got)
	default:
	}
}

// TestStaleRunDoneKeepsNextRunInFlight verifies that a RunDone from a
// cancelled run does not end the run the user started after the cancel.
func TestStaleRunDoneKeepsNextRunInFlight(t *testing.T) {
	m := New(nil, "main")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	typeRunes(m, "first")
	pressKey(m, tea.KeyEnter)

	pressKey(m, tea.KeyCtrlC)
	main := m.sessions["main"]
	if main.InFlight {
		t.Fatal("main still in flight right after cancel")
	}

	typeRunes(m, "second")
	pressKey(m, tea.KeyEnter)
	if !main.InFlight {
		t.Fatal("second message did not start a run")
	}
	// A steer is not possible without a frontend, so this goes to the queue.
	typeRunes(m, "third")
	pressKey(m, tea.KeyEnter)

	// The cancelled run's RunDone arrives late.
	sendEvent(m, "main", agent.EventRunDone)

	if !main.InFlight {
		t.Error("stale RunDone cleared InFlight for the next run")
	}
	if !m.spinner.Active() {
		t.Error("stale RunDone stopped the spinner")
	}
	if len(m.pendingMessages) != 1 {
		t.Errorf("stale RunDone drained the queue: pending = %v", m.pendingMessages)
	}

	// The second run's RunDone drains the queue.
	sendEvent(m, "main", agent.EventRunDone)
	if len(m.pendingMessages) != 0 {
		t.Errorf("pending = %v, want drained", m.pendingMessages)
	}
	if !main.InFlight {
		t.Error("main not in flight after the queue was sent")
	}

	// The queued run's RunDone ends the run.
	sendEvent(m, "main", agent.EventRunDone)
	if main.InFlight || m.spinner.Active() {
		t.Errorf("after last RunDone: InFlight=%v spinner=%v, want both false", main.InFlight, m.spinner.Active())
	}
}

// TestSessionSwitchRefusedWhileInFlight verifies that /new and /resume are
// refused while the main run is in flight.
func TestSessionSwitchRefusedWhileInFlight(t *testing.T) {
	m := New(nil, "main")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	var calls int
	m.SetSlashCallbacks(SlashCallbacks{
		NewSession:   func() (string, error) { calls++; return "other", nil },
		ListSessions: func() ([]session.Listing, error) { calls++; return []session.Listing{{ID: "old"}}, nil },
	})
	typeRunes(m, "work")
	pressKey(m, tea.KeyEnter)

	for _, cmd := range []string{"/new", "/resume"} {
		typeRunes(m, cmd)
		pressKey(m, tea.KeyEnter)
		if m.focused != "main" {
			t.Errorf("%s: focused = %q, want main", cmd, m.focused)
		}
		if want := cmd + ": cancel the run first"; m.statusMsg != want {
			t.Errorf("%s: statusMsg = %q, want %q", cmd, m.statusMsg, want)
		}
		if m.activeModal != nil {
			t.Errorf("%s: a modal was opened", cmd)
		}
	}
	if calls != 0 {
		t.Errorf("session callbacks called %d times, want 0", calls)
	}
}

// TestOldMainViewReadOnlyAfterNew verifies that after /new the previous main
// view refuses Enter instead of sending a run nobody will finish.
func TestOldMainViewReadOnlyAfterNew(t *testing.T) {
	m := New(nil, "main")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.SetSlashCallbacks(SlashCallbacks{
		NewSession: func() (string, error) { return "fresh", nil },
	})
	typeRunes(m, "/new")
	pressKey(m, tea.KeyEnter)
	if m.mainID != "fresh" {
		t.Fatalf("mainID = %q, want fresh", m.mainID)
	}

	m.focused = "main"
	typeRunes(m, "hello")
	pressKey(m, tea.KeyEnter)

	if m.sessions["main"].InFlight {
		t.Error("old view is in flight")
	}
	if m.mainRuns != 0 {
		t.Errorf("mainRuns = %d, want 0", m.mainRuns)
	}
	if got := m.editor.Value(); got != "hello" {
		t.Errorf("editor = %q, want text kept", got)
	}
	if want := "switch to the main session to send"; m.statusMsg != want {
		t.Errorf("statusMsg = %q, want %q", m.statusMsg, want)
	}
}

// TestMainErrorKeepsRunInFlightWhileRunsOutstanding verifies that an error
// event does not end the run while a RunDone is still expected.
func TestMainErrorKeepsRunInFlightWhileRunsOutstanding(t *testing.T) {
	for _, kind := range []agent.EventKind{agent.EventError, agent.EventToolDenied} {
		m := New(nil, "main")
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		typeRunes(m, "work")
		pressKey(m, tea.KeyEnter)
		if m.mainRuns != 1 {
			t.Fatalf("mainRuns = %d, want 1", m.mainRuns)
		}

		sendEvent(m, "main", kind)
		if !m.mainSession().InFlight {
			t.Errorf("kind %v: main not in flight with a run outstanding", kind)
		}
		pressKey(m, tea.KeyCtrlC)
		if m.mainSession().InFlight {
			t.Errorf("kind %v: Ctrl+C did not cancel the run", kind)
		}
	}
}
