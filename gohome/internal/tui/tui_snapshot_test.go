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
