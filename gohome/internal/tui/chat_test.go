package tui

import (
	"strings"
	"testing"
)

func TestChatRenderUserMessage(t *testing.T) {
	entries := []TimelineEntry{{Kind: "user", Text: "hello world"}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "hello world") {
		t.Errorf("user message not found in render: %q", joined)
	}
}

func TestChatRenderAssistantMarkdown(t *testing.T) {
	entries := []TimelineEntry{{Kind: "assistant", Text: "# Hello\n\nThis is **bold**."}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, ansiBold) {
		t.Error("expected bold ANSI in heading")
	}
	plain := StripAnsi(joined)
	if !strings.Contains(plain, "Hello") {
		t.Errorf("heading text missing: %q", plain)
	}
}

func TestChatRenderToolCollapsed(t *testing.T) {
	entries := []TimelineEntry{{Kind: "tool", ToolName: "bash", Text: `{"command":"ls"}`, ToolResult: "file.txt"}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "bash") {
		t.Errorf("tool name missing: %q", joined)
	}
}

func TestChatRenderEmpty(t *testing.T) {
	entries := []TimelineEntry{}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	if len(lines) != 0 {
		t.Errorf("empty timeline should render 0 lines, got %d", len(lines))
	}
}

func TestChatScrolling(t *testing.T) {
	var entries []TimelineEntry
	for i := 0; i < 50; i++ {
		entries = append(entries, TimelineEntry{Kind: "user", Text: "message"})
	}
	c := NewChat(&entries, 10)
	lines := c.Render(80)
	if len(lines) > 10 {
		t.Errorf("expected max 10 lines, got %d", len(lines))
	}
}

func TestToolStatusPending(t *testing.T) {
	entries := []TimelineEntry{{
		Kind:     "tool",
		ToolName: "bash",
		Text:     `{"command":"ls"}`,
		Status:   "pending",
	}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "bash") {
		t.Errorf("tool name not found: %q", joined)
	}
}

func TestToolStatusSuccess(t *testing.T) {
	entries := []TimelineEntry{{
		Kind:       "tool",
		ToolName:   "bash",
		Text:       `{"command":"ls"}`,
		ToolResult: "file.txt",
		Status:     "success",
	}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "bash") {
		t.Errorf("tool name not found: %q", joined)
	}
}

func TestToolStatusError(t *testing.T) {
	entries := []TimelineEntry{{
		Kind:       "tool",
		ToolName:   "bash",
		Text:       `{"command":"rm /"}`,
		ToolResult: "permission denied",
		Status:     "error",
	}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "ERROR") {
		t.Errorf("error prefix not found: %q", joined)
	}
}
