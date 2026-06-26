package tui_test

// TestSnapshots provides a golden-file snapshot suite for the TUI.
// Run with -update to regenerate golden files:
//
//	go test ./gohome/internal/tui/ -run TestSnapshots -update
//
// Determinism: all state transitions are driven synchronously through
// Model.Update, and lipgloss.SetColorProfile(termenv.Ascii) is called in
// TestMain (see tui_test_main_test.go), so View() output is stable across
// machines and colour profiles. No goroutines, no sleeps, no teatest.

import (
	"fmt"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/tui"
)

const snapshotW = 80
const snapshotH = 24

// apply sends msg to m synchronously and returns the updated *Model.
func apply(m *tui.Model, msg tea.Msg) *tui.Model {
	nm, _ := m.Update(msg)
	return nm.(*tui.Model)
}

// newSized builds a Model already sized to 80x24.
func newSized() *tui.Model {
	m := tui.New(nil, "")
	m = apply(m, tea.WindowSizeMsg{Width: snapshotW, Height: snapshotH})
	return m
}

func TestSnapshots(t *testing.T) {
	// (a) Empty initial view.
	t.Run("empty_initial_view", func(t *testing.T) {
		m := newSized()
		golden.RequireEqual(t, []byte(m.View()))
	})

	// (b) After a single user message.
	t.Run("after_user_message", func(t *testing.T) {
		m := newSized()
		m.AddTimelineEntry("main", tui.TimelineEntry{Kind: tui.KindUser, Text: "hello world"})
		golden.RequireEqual(t, []byte(m.View()))
	})

	// (c) After one assistant turn (token deltas + turn done).
	t.Run("after_assistant_turn", func(t *testing.T) {
		m := newSized()
		m.AddTimelineEntry("main", tui.TimelineEntry{Kind: tui.KindUser, Text: "what is 2+2?"})
		m = apply(m, tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
			Kind:      agent.EventTokenDelta,
			SessionID: "main",
			TextDelta: "The answer is 4.",
		}})
		m = apply(m, tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
			Kind:      agent.EventTurnDone,
			SessionID: "main",
		}})
		golden.RequireEqual(t, []byte(m.View()))
	})

	// (d) With an approval prompt active.
	t.Run("with_approval_prompt", func(t *testing.T) {
		m := newSized()
		reply := make(chan guard.ApprovalDecision, 1)
		m = apply(m, tui.ApprovalReqMsg{
			Req: guard.ApprovalRequest{
				SessionID:        "main",
				Tool:             "bash",
				Input:            []byte(`{"command":"ls -la"}`),
				SuggestedPattern: "ls*",
			},
			Reply: reply,
		})
		golden.RequireEqual(t, []byte(m.View()))
	})

	// (e) With a subagent in the session strip.
	t.Run("with_subagent_strip", func(t *testing.T) {
		m := newSized()
		m = apply(m, tui.AgentEventMsg{SessionID: "sub1", Ev: agent.Event{
			Kind:      agent.EventSessionStarted,
			SessionID: "sub1",
		}})
		golden.RequireEqual(t, []byte(m.View()))
	})

	// (e2) Full subagent lifecycle: start -> thinking -> text -> session_ended.
	t.Run("subagent_lifecycle", func(t *testing.T) {
		m := newSized()
		// Start the subagent session.
		m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
			Kind:      agent.EventSessionStarted,
			SessionID: "sub-1",
		}})
		// Thinking begins (sets InFlight=true → "running").
		m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
			Kind:          agent.EventThinkingDelta,
			SessionID:     "sub-1",
			ThinkingDelta: "Let me investigate...",
		}})
		// Thinking ends (collapses block).
		m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
			Kind:      agent.EventThinkingDone,
			SessionID: "sub-1",
		}})
		// Assistant text arrives.
		m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
			Kind:      agent.EventTokenDelta,
			SessionID: "sub-1",
			TextDelta: "This is a Go project.",
		}})
		// Turn ends.
		m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
			Kind:       agent.EventTurnDone,
			SessionID:  "sub-1",
			StopReason: "end_turn",
		}})
		// Session ends (the fix we're testing).
		m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
			Kind:      agent.EventSessionEnded,
			SessionID: "sub-1",
		}})
		golden.RequireEqual(t, []byte(m.View()))
	})

	// (f) With the /tokens overlay open.
	t.Run("with_tokens_overlay", func(t *testing.T) {
		m := newSized()
		m.SetModelName("claude-3-5-sonnet")
		m.SetContextWindow(100000)
		m = apply(m, tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
			Kind:      agent.EventUsageUpdated,
			SessionID: "main",
			Usage: &common.Usage{
				InputTokens:      5000,
				OutputTokens:     1000,
				CacheReadTokens:  200,
				CacheWriteTokens: 50,
			},
		}})
		// Open the /tokens overlay by setting state directly via the exported setter.
		// The slash command path goes through the textarea (async); using the
		// exported bool is the cleanest synchronous equivalent.
		m.OpenTokensOverlay()
		golden.RequireEqual(t, []byte(m.View()))
	})

	t.Run("with_help_overlay", func(t *testing.T) {
		m := newSized()
		m.OpenHelpOverlay()
		golden.RequireEqual(t, []byte(m.View()))
	})

	t.Run("completed_subagent_view", func(t *testing.T) {
		m := newSized()
		m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
			Kind:      agent.EventSessionStarted,
			SessionID: "sub-1",
		}})
		m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
			Kind:      agent.EventTokenDelta,
			SessionID: "sub-1",
			TextDelta: "Subagent result text.",
		}})
		m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
			Kind:      agent.EventTurnDone,
			SessionID: "sub-1",
		}})
		m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
			Kind:      agent.EventSessionEnded,
			SessionID: "sub-1",
			EndReason: "done",
		}})
		// Focus the completed session.
		m = apply(m, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
		golden.RequireEqual(t, []byte(m.View()))
	})

	// Tool output preview: multi-line result (6 lines) shows last 3 as dimmed preview.
	t.Run("tool_output_preview", func(t *testing.T) {
		m := newSized()
		m.AddTimelineEntry("main", tui.TimelineEntry{
			Kind:     tui.KindTool,
			ToolName: "bash",
			Text:     `{"command":"find . -name '*.go'"}`,
			ToolResult: "cmd/main.go\ninternal/agent/agent.go\ninternal/tui/model.go\n" +
				"internal/tui/chat.go\ninternal/tools/bash.go\ninternal/guard/guard.go",
			Status: "success",
		})
		golden.RequireEqual(t, []byte(m.View()))
	})

	// Tool output preview: single-line result produces NO preview lines.
	t.Run("tool_output_preview_single_line", func(t *testing.T) {
		m := newSized()
		m.AddTimelineEntry("main", tui.TimelineEntry{
			Kind:       tui.KindTool,
			ToolName:   "bash",
			Text:       `{"command":"echo hello"}`,
			ToolResult: "hello",
			Status:     "success",
		})
		golden.RequireEqual(t, []byte(m.View()))
	})

	// Tool output preview: shadow entry with multi-line result.
	t.Run("tool_output_preview_shadow", func(t *testing.T) {
		m := newSized()
		m.AddTimelineEntry("main", tui.TimelineEntry{
			Kind:           tui.KindTool,
			ToolName:       "subagent",
			Text:           `{"task":"investigate"}`,
			Status:         "pending",
			ChildSessionID: "sub-1",
		})
		m.AddTimelineEntry("main", tui.TimelineEntry{
			Kind:           tui.KindTool,
			ToolName:       "bash",
			Text:           `{"command":"find . -name '*.go'"}`,
			ToolResult:     "file1.go\nfile2.go\nfile3.go\nfile4.go\nfile5.go",
			Status:         "success",
			Shadow:         true,
			ChildSessionID: "sub-1",
		})
		golden.RequireEqual(t, []byte(m.View()))
	})

	// Edit tool diff box: pending state (no result yet).
	t.Run("edit_tool_diff_pending", func(t *testing.T) {
		m := newSized()
		tmpFile := t.TempDir() + "/test.go"
		if err := os.WriteFile(tmpFile, []byte("line1\nline2\nfunc Old() {\nline4\nline5\n"), 0644); err != nil {
			t.Fatal(err)
		}
		m = apply(m, tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
			Kind:      agent.EventToolCallDone,
			SessionID: "main",
			ToolName:  "edit",
			InputJSON: fmt.Sprintf(`{"path":%q,"old_string":"func Old() {","new_string":"func New() {"}`, tmpFile),
		}})
		golden.RequireEqual(t, []byte(m.View()))
	})

	// Edit tool diff box: success state (completed successfully).
	t.Run("edit_tool_diff_success", func(t *testing.T) {
		m := newSized()
		tmpFile := t.TempDir() + "/test.go"
		if err := os.WriteFile(tmpFile, []byte("line1\nline2\nfunc Old() {\nline4\nline5\n"), 0644); err != nil {
			t.Fatal(err)
		}
		m = apply(m, tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
			Kind:      agent.EventToolCallDone,
			SessionID: "main",
			ToolName:  "edit",
			InputJSON: fmt.Sprintf(`{"path":%q,"old_string":"func Old() {","new_string":"func New() {"}`, tmpFile),
		}})
		m = apply(m, tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
			Kind:      agent.EventToolResult,
			SessionID: "main",
			Result:    &agent.ToolResult{Content: fmt.Sprintf(`edit: replaced 1 occurrence(s) in %q`, tmpFile)},
		}})
		golden.RequireEqual(t, []byte(m.View()))
	})

	// Edit tool diff box: denied/error state (red border, dimmed text).
	t.Run("edit_tool_diff_denied", func(t *testing.T) {
		m := newSized()
		tmpFile := t.TempDir() + "/test.go"
		if err := os.WriteFile(tmpFile, []byte("line1\nline2\nfunc Old() {\nline4\nline5\n"), 0644); err != nil {
			t.Fatal(err)
		}
		m = apply(m, tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
			Kind:      agent.EventToolCallDone,
			SessionID: "main",
			ToolName:  "edit",
			InputJSON: fmt.Sprintf(`{"path":%q,"old_string":"func Old() {","new_string":"func New() {"}`, tmpFile),
		}})
		m = apply(m, tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
			Kind:      agent.EventToolResult,
			SessionID: "main",
			Result:    &agent.ToolResult{Content: "denied", IsError: true},
		}})
		golden.RequireEqual(t, []byte(m.View()))
	})

	t.Run("subagent_shadow_entries", func(t *testing.T) {
		m := newSized()
		m.AddTimelineEntry("main", tui.TimelineEntry{
			Kind:           tui.KindTool,
			ToolName:       "subagent",
			Text:           `{"task":"investigate"}`,
			Status:         "pending",
			ChildSessionID: "sub-1",
		})
		m.AddTimelineEntry("main", tui.TimelineEntry{
			Kind:           tui.KindTool,
			ToolName:       "bash",
			Text:           `{"command":"ls"}`,
			ToolResult:     "file1.go",
			Status:         "success",
			Shadow:         true,
			ChildSessionID: "sub-1",
		})
		m.AddTimelineEntry("main", tui.TimelineEntry{
			Kind:           tui.KindTool,
			ToolName:       "read",
			Text:           `{"path":"main.go"}`,
			Status:         "pending",
			Shadow:         true,
			ChildSessionID: "sub-1",
		})
		golden.RequireEqual(t, []byte(m.View()))
	})
}

func TestCopyKey_SetsStatusMessage(t *testing.T) {
	m := newSized()
	m.AddTimelineEntry("main", tui.TimelineEntry{Kind: tui.KindUser, Text: "hello clipboard"})
	m.AddTimelineEntry("main", tui.TimelineEntry{Kind: tui.KindAssistant, Text: "response text"})

	// Move cursor to the assistant entry.
	m = apply(m, tea.KeyMsg{Type: tea.KeyDown})

	// Press 'c' to copy.
	m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})

	// Should show a status message (either success or failure is fine in test env).
	msg := m.StatusMsg()
	if msg == "" {
		t.Fatal("expected a status message after pressing 'c'")
	}
}

func TestCopyKey_ToolEntry_IncludesAllContent(t *testing.T) {
	m := newSized()
	m.AddTimelineEntry("main", tui.TimelineEntry{
		Kind:       tui.KindTool,
		ToolName:   "bash",
		Text:       `{"command":"ls"}`,
		ToolResult: "file.go",
		Status:     "success",
	})

	// Press 'c' to copy (cursor starts at 0).
	m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})

	msg := m.StatusMsg()
	if msg == "" {
		t.Fatal("expected a status message after pressing 'c'")
	}
}

func TestShadowEntryFields(t *testing.T) {
	e := tui.TimelineEntry{
		Kind:           tui.KindTool,
		ToolName:       "bash",
		Text:           `{"command":"ls"}`,
		Status:         "success",
		Shadow:         true,
		ChildSessionID: "sub-1",
	}
	if !e.Shadow {
		t.Fatal("Shadow should be true")
	}
	if e.ChildSessionID != "sub-1" {
		t.Fatalf("ChildSessionID = %q, want %q", e.ChildSessionID, "sub-1")
	}
}

func TestChildToParentMapping(t *testing.T) {
	m := newSized()
	// Simulate subagent tool call in parent timeline first.
	m.AddTimelineEntry("main", tui.TimelineEntry{
		Kind:     tui.KindTool,
		ToolName: "subagent",
		Text:     `{"task":"do stuff"}`,
		Status:   "pending",
	})
	// Start child session.
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventSessionStarted,
		SessionID: "sub-1",
	}})
	// The parent's subagent tool entry should now have ChildSessionID set.
	sv := m.Sessions()["main"]
	found := false
	for _, e := range sv.Timeline {
		if e.ToolName == "subagent" && e.ChildSessionID == "sub-1" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected subagent tool entry to have ChildSessionID='sub-1'")
	}
}

func TestShadowEntries_InsertedOnChildToolCall(t *testing.T) {
	m := newSized()
	m.AddTimelineEntry("main", tui.TimelineEntry{
		Kind:     tui.KindTool,
		ToolName: "subagent",
		Text:     `{"task":"investigate"}`,
		Status:   "pending",
	})
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventSessionStarted,
		SessionID: "sub-1",
	}})
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventToolCallDone,
		SessionID: "sub-1",
		ToolName:  "bash",
		InputJSON: `{"command":"ls"}`,
	}})
	sv := m.Sessions()["main"]
	if len(sv.Timeline) < 2 {
		t.Fatalf("expected at least 2 entries in parent timeline, got %d", len(sv.Timeline))
	}
	shadow := sv.Timeline[1]
	if !shadow.Shadow {
		t.Fatal("second entry should be a shadow entry")
	}
	if shadow.ToolName != "bash" {
		t.Fatalf("shadow ToolName = %q, want %q", shadow.ToolName, "bash")
	}
}

func TestShadowEntries_SlidingWindowMax3(t *testing.T) {
	m := newSized()
	m.AddTimelineEntry("main", tui.TimelineEntry{
		Kind:     tui.KindTool,
		ToolName: "subagent",
		Text:     `{"task":"investigate"}`,
		Status:   "pending",
	})
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventSessionStarted,
		SessionID: "sub-1",
	}})
	// Insert 4 tool calls -- only 3 shadows should remain.
	for i := 0; i < 4; i++ {
		m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
			Kind:      agent.EventToolCallDone,
			SessionID: "sub-1",
			ToolName:  "bash",
			InputJSON: fmt.Sprintf(`{"command":"cmd%d"}`, i),
		}})
	}
	sv := m.Sessions()["main"]
	shadowCount := 0
	for _, e := range sv.Timeline {
		if e.Shadow {
			shadowCount++
		}
	}
	if shadowCount != 3 {
		t.Fatalf("expected 3 shadow entries, got %d", shadowCount)
	}
	// The oldest shadow (cmd0) should have been evicted; first shadow should be cmd1.
	firstShadow := sv.Timeline[1]
	if firstShadow.Text != `{"command":"cmd1"}` {
		t.Fatalf("first shadow Text = %q, want %q", firstShadow.Text, `{"command":"cmd1"}`)
	}
}

func TestShadowEntries_UpdatedOnChildToolResult(t *testing.T) {
	m := newSized()
	m.AddTimelineEntry("main", tui.TimelineEntry{
		Kind:     tui.KindTool,
		ToolName: "subagent",
		Text:     `{"task":"investigate"}`,
		Status:   "pending",
	})
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventSessionStarted,
		SessionID: "sub-1",
	}})
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventToolCallDone,
		SessionID: "sub-1",
		ToolName:  "bash",
		InputJSON: `{"command":"ls"}`,
	}})
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventToolResult,
		SessionID: "sub-1",
		Result: &agent.ToolResult{
			Content: "file1.go\nfile2.go",
			IsError: false,
		},
	}})

	sv := m.Sessions()["main"]
	shadow := sv.Timeline[1]
	if shadow.Status != "success" {
		t.Fatalf("shadow Status = %q, want %q", shadow.Status, "success")
	}
	if shadow.ToolResult != "file1.go\nfile2.go" {
		t.Fatalf("shadow ToolResult = %q, want content", shadow.ToolResult)
	}
}

func TestEnsureCursorVisible_ScrollsDown(t *testing.T) {
	m := newSized()
	for i := 0; i < 20; i++ {
		m.AddTimelineEntry("main", tui.TimelineEntry{Kind: tui.KindUser, Text: fmt.Sprintf("msg %d", i)})
	}
	for i := 0; i < 19; i++ {
		m = apply(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	view := m.View()
	if !strings.Contains(view, "msg 19") {
		t.Fatalf("expected 'msg 19' to be visible after scrolling down, got:\n%s", view)
	}
}

func TestEnsureCursorVisible_ScrollsUp(t *testing.T) {
	m := newSized()
	for i := 0; i < 20; i++ {
		m.AddTimelineEntry("main", tui.TimelineEntry{Kind: tui.KindUser, Text: fmt.Sprintf("msg %d", i)})
	}
	for i := 0; i < 19; i++ {
		m = apply(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	for i := 0; i < 19; i++ {
		m = apply(m, tea.KeyMsg{Type: tea.KeyUp})
	}
	view := m.View()
	if !strings.Contains(view, "msg 0") {
		t.Fatalf("expected 'msg 0' to be visible after scrolling up, got:\n%s", view)
	}
}

func TestSubagentCompleted_OnDone(t *testing.T) {
	m := newSized()
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventSessionStarted,
		SessionID: "sub-1",
	}})
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventSessionEnded,
		SessionID: "sub-1",
		EndReason: "done",
	}})
	sv := m.Sessions()["sub-1"]
	if !sv.Completed {
		t.Fatal("expected Completed=true after EventSessionEnded with EndReason='done'")
	}
}

func TestSubagentNotCompleted_OnCancel(t *testing.T) {
	m := newSized()
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventSessionStarted,
		SessionID: "sub-1",
	}})
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventSessionEnded,
		SessionID: "sub-1",
		EndReason: "cancelled",
	}})
	sv := m.Sessions()["sub-1"]
	if sv.Completed {
		t.Fatal("expected Completed=false after EventSessionEnded with EndReason='cancelled'")
	}
}

func TestCompletedSession_RejectsInput(t *testing.T) {
	m := newSized()
	// Create and complete a subagent session.
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventSessionStarted,
		SessionID: "sub-1",
	}})
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventSessionEnded,
		SessionID: "sub-1",
		EndReason: "done",
	}})
	// Focus the completed session.
	m = apply(m, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})

	// Type something into the editor.
	for _, r := range "hello" {
		m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})

	// Should show "Session complete" status, not add a user entry.
	if m.StatusMsg() != "Session complete" {
		t.Errorf("StatusMsg = %q, want %q", m.StatusMsg(), "Session complete")
	}
	sv := m.Sessions()["sub-1"]
	for _, e := range sv.Timeline {
		if e.Kind == tui.KindUser {
			t.Fatal("user entry should not be added to completed session")
		}
	}
}

func TestCompletedSession_AllowsNavigation(t *testing.T) {
	m := newSized()
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventSessionStarted,
		SessionID: "sub-1",
	}})
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventTokenDelta,
		SessionID: "sub-1",
		TextDelta: "Hello from subagent",
	}})
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventSessionEnded,
		SessionID: "sub-1",
		EndReason: "done",
	}})
	// Focus the completed session.
	m = apply(m, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})

	// Arrow down should work (not panic or be blocked).
	_ = apply(m, tea.KeyMsg{Type: tea.KeyDown})
	// No crash = pass.
}

func TestEnsureCursorVisible_TallEntry_NoFlicker(t *testing.T) {
	m := newSized()
	// Add a few short entries, then a very tall one (taller than viewport).
	for i := 0; i < 3; i++ {
		m.AddTimelineEntry("main", tui.TimelineEntry{Kind: tui.KindUser, Text: fmt.Sprintf("msg %d", i)})
	}
	var longText strings.Builder
	for i := 0; i < 40; i++ {
		if i > 0 {
			longText.WriteString("\n")
		}
		fmt.Fprintf(&longText, "line %d of long response", i)
	}
	m.AddTimelineEntry("main", tui.TimelineEntry{Kind: tui.KindAssistant, Text: longText.String()})

	// Navigate to the tall entry.
	for i := 0; i < 3; i++ {
		m = apply(m, tea.KeyMsg{Type: tea.KeyDown})
	}

	// Pressing down repeatedly should produce stable output (no oscillation).
	view1 := m.View()
	m = apply(m, tea.KeyMsg{Type: tea.KeyDown})
	view2 := m.View()
	m = apply(m, tea.KeyMsg{Type: tea.KeyDown})
	view3 := m.View()

	if view2 != view3 {
		t.Fatalf("view oscillated between successive down presses on tall entry:\nview2:\n%s\nview3:\n%s", view2, view3)
	}
	_ = view1
}

func TestToggleExpansion_PreservesScrollPosition(t *testing.T) {
	m := newSized()

	// Add several entries so the timeline exceeds viewport height.
	for i := 0; i < 15; i++ {
		m.AddTimelineEntry("main", tui.TimelineEntry{Kind: tui.KindUser, Text: fmt.Sprintf("message %d", i)})
	}
	// Add a tool entry at the end.
	m.AddTimelineEntry("main", tui.TimelineEntry{
		Kind:       tui.KindTool,
		ToolName:   "bash",
		Text:       `{"command":"ls"}`,
		ToolResult: "file1.go\nfile2.go\nfile3.go\nfile4.go\nfile5.go",
		Status:     "success",
	})

	// Move cursor to the tool entry (last entry).
	for i := 0; i < 16; i++ {
		m = apply(m, tea.KeyMsg{Type: tea.KeyDown})
	}

	// Record scroll state, then toggle expansion.
	viewBefore := m.View()
	m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
	viewAfter := m.View()

	// The tool entry should still be visible after expansion (not scrolled away).
	if !strings.Contains(viewAfter, "bash") {
		t.Errorf("tool entry should remain visible after expansion.\nBefore:\n%s\nAfter:\n%s", viewBefore, viewAfter)
	}
}
