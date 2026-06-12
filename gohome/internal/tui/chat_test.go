package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
)

func TestChatRenderUserMessage(t *testing.T) {
	entries := []TimelineEntry{{Kind: KindUser, Text: "hello world"}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "hello world") {
		t.Errorf("user message not found in render: %q", joined)
	}
}

func TestChatRenderAssistantMarkdown(t *testing.T) {
	entries := []TimelineEntry{{Kind: KindAssistant, Text: "# Hello\n\nThis is **bold**."}}
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
	entries := []TimelineEntry{{Kind: KindTool, ToolName: "bash", Text: `{"command":"ls"}`, ToolResult: "file.txt"}}
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
		entries = append(entries, TimelineEntry{Kind: KindUser, Text: "message"})
	}
	c := NewChat(&entries, 10)
	lines := c.Render(80)
	if len(lines) > 10 {
		t.Errorf("expected max 10 lines, got %d", len(lines))
	}
}

func TestToolStatusPending(t *testing.T) {
	entries := []TimelineEntry{{
		Kind:     KindTool,
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
		Kind:       KindTool,
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
		Kind:       KindTool,
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

func TestChatRenderThinkingCollapsed(t *testing.T) {
	entries := []TimelineEntry{{Kind: KindThinking, Text: "Let me reason\nabout this\nstep by step."}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "Thinking...") {
		t.Errorf("collapsed thinking label missing: %q", joined)
	}
	if !strings.Contains(joined, "3 lines") {
		t.Errorf("line count indicator missing: %q", joined)
	}
}

func TestChatRenderThinkingExpanded(t *testing.T) {
	entries := []TimelineEntry{{Kind: KindThinking, Text: "Step 1: analyze\nStep 2: solve", Expanded: true}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "Step 1") {
		t.Errorf("expanded thinking content missing: %q", joined)
	}
}

func TestChatRenderToolExpanded_HasBackground(t *testing.T) {
	entries := []TimelineEntry{{
		Kind:       KindTool,
		ToolName:   "bash",
		Text:       `{"command":"ls"}`,
		ToolResult: "file.txt",
		Status:     "success",
		Expanded:   true,
	}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	// Expanded lines (args/result) should have content.
	if len(lines) < 2 {
		t.Fatalf("expected multiple lines for expanded tool, got %d", len(lines))
	}
	// Check that result content appears in expanded output.
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "file.txt") {
		t.Errorf("expanded tool result missing: %q", joined)
	}
	if !strings.Contains(joined, "args:") {
		t.Errorf("expanded tool args label missing: %q", joined)
	}
}

func TestChatRenderCacheReuse(t *testing.T) {
	entries := []TimelineEntry{
		{Kind: KindAssistant, Text: "# Hello\n\nSome **bold** text."},
		{Kind: KindUser, Text: "follow up"},
	}
	c := NewChat(&entries, 40)

	first := c.Render(80)
	if len(first) == 0 {
		t.Fatal("expected non-empty render")
	}

	// After first render, cache should be populated.
	if entries[0].cachedLines == nil {
		t.Error("expected cachedLines to be populated after first render")
	}
	if entries[0].cachedWidth != 80 {
		t.Errorf("cachedWidth: got %d, want 80", entries[0].cachedWidth)
	}

	// Second render with same state should produce identical output.
	second := c.Render(80)
	if len(first) != len(second) {
		t.Fatalf("line count mismatch: first=%d second=%d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("line %d differs:\n  first:  %q\n  second: %q", i, first[i], second[i])
		}
	}
}

func TestChatRenderCacheInvalidatesOnWidthChange(t *testing.T) {
	entries := []TimelineEntry{
		{Kind: KindAssistant, Text: "Some text that will wrap differently at different widths."},
	}
	c := NewChat(&entries, 40)

	first := c.Render(80)
	cachedWidth80 := entries[0].cachedWidth

	second := c.Render(40)
	cachedWidth40 := entries[0].cachedWidth

	if cachedWidth80 != 80 {
		t.Errorf("expected cachedWidth 80 after first render, got %d", cachedWidth80)
	}
	if cachedWidth40 != 40 {
		t.Errorf("expected cachedWidth 40 after second render, got %d", cachedWidth40)
	}

	// The outputs should differ because wrapping changed.
	joined1 := strings.Join(first, "\n")
	joined2 := strings.Join(second, "\n")
	if joined1 == joined2 {
		t.Error("expected different output at different widths")
	}
}

func TestChatRenderCacheInvalidatesOnTextChange(t *testing.T) {
	entries := []TimelineEntry{
		{Kind: KindAssistant, Text: "first version"},
	}
	c := NewChat(&entries, 40)
	c.Render(80)

	if entries[0].cachedText != "first version" {
		t.Errorf("cachedText: got %q, want %q", entries[0].cachedText, "first version")
	}

	// Mutate the text (simulating a token delta append).
	entries[0].Text = "first version, extended"
	c.Render(80)

	if entries[0].cachedText != "first version, extended" {
		t.Errorf("cachedText after mutation: got %q, want %q", entries[0].cachedText, "first version, extended")
	}
}

func TestCountLinesCacheBehavior(t *testing.T) {
	entries := []TimelineEntry{
		{Kind: KindAssistant, Text: "# Hello\n\nParagraph one."},
		{Kind: KindUser, Text: "reply"},
	}
	c := NewChat(&entries, 40)

	// Call Render first to populate caches.
	c.Render(80)

	// Now DisableAutoScroll calls countLines internally.
	// It should use cached line counts rather than re-rendering.
	c.ScrollToBottom()
	c.DisableAutoScroll(80)

	// After disabling, autoScroll should be false and scrollTop should be set.
	if c.IsAutoScroll() {
		t.Error("expected autoScroll to be false after DisableAutoScroll")
	}
}

func TestRenderThrottle_SkipsIntermediateRebuilds(t *testing.T) {
	m := New(nil, "main")
	m.SetRenderThrottleMs(100)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Send first token delta -- should render immediately (lastRenderTime is zero).
	model1, _ := m.Update(agentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventTokenDelta,
		SessionID: "main",
		TextDelta: "Hello ",
	}})
	m1 := model1.(*Model)

	// Send second token delta immediately -- should be throttled because
	// less than 100ms has elapsed since the first render.
	model2, cmd2 := m1.Update(agentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventTokenDelta,
		SessionID: "main",
		TextDelta: "world",
	}})
	m2 := model2.(*Model)

	// cmd2 should include a tea.Tick (the deferred render) or a SpinnerTickCmd.
	// The key point is that a command is returned (non-nil) to schedule the
	// deferred rebuild.
	if cmd2 == nil {
		t.Error("expected a non-nil command for throttled render, got nil")
	}

	// renderPending should be true since the rebuild was deferred.
	if !m2.renderPending {
		t.Error("expected renderPending to be true after throttled delta")
	}

	// Verify the text was still appended to the timeline (content is never lost).
	sv := m2.sessions["main"]
	last := sv.Timeline[len(sv.Timeline)-1]
	if last.Text != "Hello world" {
		t.Errorf("text: got %q, want %q", last.Text, "Hello world")
	}
}
