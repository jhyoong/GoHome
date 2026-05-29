package tui_test

import (
	"bytes"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/tui"
)

// TestToolCallCollapsedLine sends EventToolCallDone + EventToolResult and
// asserts the collapsed line shows the tool name.
func TestToolCallCollapsedLine(t *testing.T) {
	m := tui.New(nil)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	tm.Send(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventToolCallDone,
		SessionID: "main",
		ToolName:  "read_file",
		InputJSON: `{"path": "foo.go"}`,
	}})
	tm.Send(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventToolResult,
		SessionID: "main",
		Result: &agent.ToolResult{
			Content: "line1\nline2\nline3",
		},
	}})

	// The collapsed line must show the tool name.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("read_file"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

// TestToolCallExpansionToggle sends a tool event, moves cursor to it (Up with
// empty input), presses Enter to expand, and asserts expanded content appears.
func TestToolCallExpansionToggle(t *testing.T) {
	m := tui.New(nil)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	// Populate a tool entry.
	tm.Send(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventToolCallDone,
		SessionID: "main",
		ToolName:  "bash",
		InputJSON: `{"command": "ls -la"}`,
	}})
	tm.Send(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventToolResult,
		SessionID: "main",
		Result: &agent.ToolResult{
			Content: "file1.txt\nfile2.txt\nfile3.txt",
		},
	}})

	// Wait for the collapsed line to appear.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("bash"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// With empty input, Up moves cursor to the tool entry; then Enter expands it.
	// (Cursor starts at 0 for the first entry.)
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// After Enter, the expanded result should appear.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("result:"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
