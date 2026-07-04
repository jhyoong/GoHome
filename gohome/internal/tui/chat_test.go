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
	if !strings.Contains(joined, "$ ls") {
		t.Errorf("tool summary missing: %q", joined)
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
	if !strings.Contains(joined, "$ ls") {
		t.Errorf("tool summary not found: %q", joined)
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
	if !strings.Contains(joined, "$ ls") {
		t.Errorf("tool summary not found: %q", joined)
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

func TestChatRenderThinkingAlwaysVisible(t *testing.T) {
	entries := []TimelineEntry{{Kind: KindThinking, Text: "Let me reason\nabout this\nstep by step."}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	// Content should always be visible inline (no collapsed label).
	if !strings.Contains(joined, "Let me reason") {
		t.Errorf("thinking content not visible: %q", joined)
	}
	if !strings.Contains(joined, "step by step") {
		t.Errorf("thinking content not fully visible: %q", joined)
	}
	if strings.Contains(joined, "Thinking...") {
		t.Errorf("should not contain collapsed label: %q", joined)
	}
}

func TestChatRenderThinkingAlwaysVisibleRegardlessOfExpanded(t *testing.T) {
	// Expanded field is ignored for KindThinking; content always shown.
	entries := []TimelineEntry{{Kind: KindThinking, Text: "Step 1: analyze\nStep 2: solve", Expanded: true}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "Step 1") {
		t.Errorf("thinking content missing: %q", joined)
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

func TestRenderToolSummary(t *testing.T) {
	tests := []struct {
		name     string
		entry    TimelineEntry
		maxWidth int
		wantSub  string // substring expected in stripped output
	}{
		{
			name: "bash command",
			entry: TimelineEntry{
				Kind:       KindTool,
				ToolName:   "bash",
				Text:       `{"command":"ls -la"}`,
				ToolResult: "27 lines",
				Status:     "success",
			},
			maxWidth: 80,
			wantSub:  "$ ls -la -> 27 lines",
		},
		{
			name: "read file_path",
			entry: TimelineEntry{
				Kind:       KindTool,
				ToolName:   "read",
				Text:       `{"file_path":"/src/main.go"}`,
				ToolResult: "200 lines",
				Status:     "success",
			},
			maxWidth: 80,
			wantSub:  "/src/main.go -> 200 lines",
		},
		{
			name: "write file_path",
			entry: TimelineEntry{
				Kind:       KindTool,
				ToolName:   "write",
				Text:       `{"file_path":"/tmp/out.txt","content":"hello"}`,
				ToolResult: "ok",
				Status:     "success",
			},
			maxWidth: 80,
			wantSub:  "write /tmp/out.txt -> ok",
		},
		{
			name: "edit file_path",
			entry: TimelineEntry{
				Kind:       KindTool,
				ToolName:   "edit",
				Text:       `{"file_path":"/src/foo.go","old_string":"a","new_string":"b"}`,
				ToolResult: "applied",
				Status:     "success",
			},
			maxWidth: 80,
			wantSub:  "edit /src/foo.go -> applied",
		},
		{
			name: "subagent prompt",
			entry: TimelineEntry{
				Kind:       KindTool,
				ToolName:   "subagent",
				Text:       `{"prompt":"Refactor the logging module"}`,
				ToolResult: "done",
				Status:     "success",
			},
			maxWidth: 80,
			wantSub:  "subagent: Refactor the logging module -> done",
		},
		{
			name: "unknown tool",
			entry: TimelineEntry{
				Kind:       KindTool,
				ToolName:   "custom_tool",
				Text:       `{"query":"find stuff"}`,
				ToolResult: "3 results",
				Status:     "success",
			},
			maxWidth: 80,
			wantSub:  "custom_tool:",
		},
		{
			name: "invalid json fallback",
			entry: TimelineEntry{
				Kind:       KindTool,
				ToolName:   "bash",
				Text:       `not valid json`,
				ToolResult: "error",
				Status:     "error",
			},
			maxWidth: 80,
			wantSub:  "$ not valid json -> ERROR: error",
		},
		{
			name: "error status",
			entry: TimelineEntry{
				Kind:       KindTool,
				ToolName:   "bash",
				Text:       `{"command":"rm -rf /"}`,
				ToolResult: "permission denied",
				Status:     "error",
			},
			maxWidth: 80,
			wantSub:  "$ rm -rf / -> ERROR: permission denied",
		},
		{
			name: "pending status no result",
			entry: TimelineEntry{
				Kind:     KindTool,
				ToolName: "bash",
				Text:     `{"command":"sleep 10"}`,
				Status:   "pending",
			},
			maxWidth: 80,
			wantSub:  "$ sleep 10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripAnsi(renderToolSummary(tt.entry, tt.maxWidth))
			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("renderToolSummary() = %q, want substring %q", got, tt.wantSub)
			}
		})
	}
}

func TestExtractToolArg(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		input    string
		want     string
	}{
		{"bash", "bash", `{"command":"echo hi"}`, "echo hi"},
		{"read", "read", `{"file_path":"/foo/bar.go"}`, "/foo/bar.go"},
		{"write", "write", `{"file_path":"/x.txt","content":"data"}`, "/x.txt"},
		{"edit", "edit", `{"file_path":"/a.go","old_string":"x","new_string":"y"}`, "/a.go"},
		{"subagent", "subagent", `{"prompt":"do thing"}`, "do thing"},
		{"unknown", "grep", `{"pattern":"foo"}`, `{"pattern":"foo"}`},
		{"invalid json", "bash", `not json`, "not json"},
		{"empty", "bash", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractToolArg(tt.toolName, tt.input)
			if got != tt.want {
				t.Errorf("extractToolArg(%q, %q) = %q, want %q", tt.toolName, tt.input, got, tt.want)
			}
		})
	}
}

func TestExpandHintOnTruncatedOutput(t *testing.T) {
	// 6-line result: preview shows last 3 lines + hint showing "3 earlier lines"
	entries := []TimelineEntry{{
		Kind:       KindTool,
		ToolName:   "bash",
		Text:       `{"command":"find ."}`,
		ToolResult: "line1\nline2\nline3\nline4\nline5\nline6",
		Status:     "success",
	}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "... (3 earlier lines, enter to expand)") {
		t.Errorf("expected expand hint with 3 earlier lines, got:\n%s", joined)
	}
}

func TestExpandHintOnTruncatedOutput_Shadow(t *testing.T) {
	// Shadow entry with 5-line result: preview shows last 3 + hint "2 earlier lines"
	entries := []TimelineEntry{{
		Kind:       KindTool,
		ToolName:   "bash",
		Text:       `{"command":"ls"}`,
		ToolResult: "a\nb\nc\nd\ne",
		Status:     "success",
		Shadow:     true,
	}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "... (2 earlier lines, enter to expand)") {
		t.Errorf("expected expand hint with 2 earlier lines for shadow entry, got:\n%s", joined)
	}
}

func TestNoExpandHintWhenFewLines(t *testing.T) {
	// 3-line result: all 3 lines shown in preview, no hint needed
	entries := []TimelineEntry{{
		Kind:       KindTool,
		ToolName:   "bash",
		Text:       `{"command":"ls"}`,
		ToolResult: "line1\nline2\nline3",
		Status:     "success",
	}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if strings.Contains(joined, "earlier lines") {
		t.Errorf("should NOT show expand hint for result with <= maxPreviewLines lines, got:\n%s", joined)
	}
}

func TestNoExpandHintWhenSingleLine(t *testing.T) {
	// 1-line result: no preview at all, no hint
	entries := []TimelineEntry{{
		Kind:       KindTool,
		ToolName:   "bash",
		Text:       `{"command":"echo hi"}`,
		ToolResult: "hi",
		Status:     "success",
	}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if strings.Contains(joined, "earlier lines") {
		t.Errorf("should NOT show expand hint for single-line result, got:\n%s", joined)
	}
}

func TestExpandHintLineCount(t *testing.T) {
	// Verify entryLineCount accounts for the hint line
	entries := []TimelineEntry{{
		Kind:       KindTool,
		ToolName:   "bash",
		Text:       `{"command":"find ."}`,
		ToolResult: "line1\nline2\nline3\nline4\nline5\nline6",
		Status:     "success",
	}}
	c := NewChat(&entries, 40)
	lines := c.Render(80)
	// 1 (header) + 3 (preview lines) + 1 (hint) = 5
	if len(lines) != 5 {
		t.Errorf("expected 5 lines (header + 3 preview + hint), got %d: %v", len(lines), lines)
	}
}

func TestRenderThrottle_SkipsIntermediateRebuilds(t *testing.T) {
	m := New("main")
	m.SetRenderThrottleMs(100)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Send first token delta -- should render immediately (lastRenderTime is zero).
	model1, _ := m.Update(AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventTokenDelta,
		SessionID: "main",
		TextDelta: "Hello ",
	}})
	m1 := model1.(*Model)

	// Send second token delta immediately -- should be throttled because
	// less than 100ms has elapsed since the first render.
	model2, cmd2 := m1.Update(AgentEventMsg{SessionID: "main", Ev: agent.Event{
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
