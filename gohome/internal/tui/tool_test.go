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
	m := tui.New(nil, "")
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

// TestMultipleToolCallsResultOrdering verifies that when multiple tool calls
// arrive in a single turn, results are matched to the correct entries by ID
// rather than by backwards position search.
func TestMultipleToolCallsResultOrdering(t *testing.T) {
	m := tui.New(nil, "")

	// Emit two tool calls (simulating a single LLM turn with 2 tool_use blocks).
	m.Update(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:       agent.EventToolCallDone,
		SessionID:  "main",
		ToolCallID: "tc-1",
		ToolName:   "shell",
		InputJSON:  `{"command":"ls"}`,
	}})
	m.Update(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:       agent.EventToolCallDone,
		SessionID:  "main",
		ToolCallID: "tc-2",
		ToolName:   "read_file",
		InputJSON:  `{"path":"foo.go"}`,
	}})

	// Emit results in the same order (tc-1 first, tc-2 second).
	// Without ID-based matching, tc-1's result would wrongly land on tc-2's entry.
	m.Update(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:       agent.EventToolResult,
		SessionID:  "main",
		ToolCallID: "tc-1",
		Result: &agent.ToolResult{
			ToolUseID: "tc-1",
			Content:   "shell-output",
		},
	}})
	m.Update(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:       agent.EventToolResult,
		SessionID:  "main",
		ToolCallID: "tc-2",
		Result: &agent.ToolResult{
			ToolUseID: "tc-2",
			Content:   "file-contents",
		},
	}})

	sv := m.Sessions()["main"]
	// Find the two tool entries in the timeline.
	var tools []tui.TimelineEntry
	for _, e := range sv.Timeline {
		if e.Kind == tui.KindTool {
			tools = append(tools, e)
		}
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tool entries, got %d", len(tools))
	}
	if tools[0].ToolName != "shell" || tools[0].ToolResult != "shell-output" {
		t.Errorf("tool[0]: name=%q result=%q, want shell/shell-output",
			tools[0].ToolName, tools[0].ToolResult)
	}
	if tools[1].ToolName != "read_file" || tools[1].ToolResult != "file-contents" {
		t.Errorf("tool[1]: name=%q result=%q, want read_file/file-contents",
			tools[1].ToolName, tools[1].ToolResult)
	}
}

// TestToolCallExpansionToggle sends a tool event, moves cursor to it (Up with
// empty input), presses Enter to expand, and asserts expanded content appears.
func TestToolCallExpansionToggle(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	// Populate a tool entry.
	tm.Send(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventToolCallDone,
		SessionID: "main",
		ToolName:  "shell",
		InputJSON: `{"command": "ls -la"}`,
	}})
	tm.Send(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventToolResult,
		SessionID: "main",
		Result: &agent.ToolResult{
			Content: "file1.txt\nfile2.txt\nfile3.txt",
		},
	}})

	// Wait for the collapsed line to appear (shown as "$ ls -la").
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("$ ls -la"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// With empty input, Up moves cursor to the tool entry; then Enter expands it.
	// (Cursor starts at 0 for the first entry.)
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// After Enter, the expanded result should appear.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("result:"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
