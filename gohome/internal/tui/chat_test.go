package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
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
	entries := []TimelineEntry{{Kind: KindTool, ToolName: "shell", Text: `{"command":"ls"}`, ToolResult: "file.txt"}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "$ ls") {
		t.Errorf("contextual tool display missing: %q", joined)
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
		ToolName: "shell",
		Text:     `{"command":"ls"}`,
		Status:   "pending",
	}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "$ ls") {
		t.Errorf("contextual tool display not found: %q", joined)
	}
}

func TestToolStatusSuccess(t *testing.T) {
	entries := []TimelineEntry{{
		Kind:       KindTool,
		ToolName:   "shell",
		Text:       `{"command":"ls"}`,
		ToolResult: "file.txt",
		Status:     "success",
	}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "$ ls") {
		t.Errorf("contextual tool display not found: %q", joined)
	}
}

func TestToolStatusError(t *testing.T) {
	entries := []TimelineEntry{{
		Kind:       KindTool,
		ToolName:   "shell",
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

func TestChatRenderThinkingInline(t *testing.T) {
	entries := []TimelineEntry{{Kind: KindThinking, Text: "Let me reason\nabout this\nstep by step."}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	// Thinking content is now always shown inline (dim italic), not collapsed.
	if !strings.Contains(joined, "Let me reason") {
		t.Errorf("thinking content missing: %q", joined)
	}
	if !strings.Contains(joined, "step by step") {
		t.Errorf("thinking content continuation missing: %q", joined)
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
		ToolName:   "shell",
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

func TestSmartAutoScroll_PreservesManualScroll(t *testing.T) {
	var entries []TimelineEntry
	for i := 0; i < 50; i++ {
		entries = append(entries, TimelineEntry{Kind: KindUser, Text: "message"})
	}
	c := NewChat(&entries, 10)
	c.Render(80) // populate state

	// Simulate manual scroll up
	c.DisableAutoScroll(80)
	c.ScrollUp(5)
	savedTop := c.ScrollTop()

	// Simulate what rebuildViewport does: SetTimeline + SetCursor
	// In the new behavior, this should NOT reset scroll position
	c.SetTimeline(&entries)
	c.SetCursor(-1)
	// autoScroll should still be false
	if c.IsAutoScroll() {
		t.Error("expected autoScroll to remain false after SetTimeline/SetCursor")
	}

	lines := c.Render(80)
	if len(lines) > 10 {
		t.Errorf("expected max 10 lines, got %d", len(lines))
	}
	// scrollTop should be unchanged
	if c.ScrollTop() != savedTop {
		t.Errorf("scrollTop changed: got %d, want %d", c.ScrollTop(), savedTop)
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

	// cmd2 should include the tea.Tick that schedules the deferred render.
	// (The spinner tick was already scheduled by the first delta, so it is
	// not scheduled again.)
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

func TestMouseWheelUpScrollsTimeline(t *testing.T) {
	m := New(nil, "main")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	sv := m.Sessions()["main"]
	for i := 0; i < 50; i++ {
		sv.Timeline = append(sv.Timeline, TimelineEntry{Kind: KindUser, Text: fmt.Sprintf("msg %d", i)})
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})

	if m.Chat().IsAutoScroll() {
		t.Error("expected autoScroll to be false after mouse wheel up")
	}
}

func TestMouseWheelScrollsDuringApproval(t *testing.T) {
	m := New(nil, "main")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	sv := m.Sessions()["main"]
	for i := 0; i < 50; i++ {
		sv.Timeline = append(sv.Timeline, TimelineEntry{Kind: KindUser, Text: fmt.Sprintf("msg %d", i)})
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	ch := make(chan guard.ApprovalDecision, 1)
	m.Update(ApprovalReqMsg{
		Req:   guard.ApprovalRequest{SessionID: "main", Tool: "shell", Input: json.RawMessage(`{"command":"ls"}`)},
		Reply: ch,
	})

	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})

	if m.Chat().IsAutoScroll() {
		t.Error("expected autoScroll to be false after mouse wheel up during approval")
	}
}

func TestApprovalPgUpScrollsTimeline(t *testing.T) {
	m := New(nil, "main")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Populate timeline with enough entries to need scrolling.
	sv := m.Sessions()["main"]
	for i := 0; i < 50; i++ {
		sv.Timeline = append(sv.Timeline, TimelineEntry{Kind: KindUser, Text: fmt.Sprintf("msg %d", i)})
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24}) // trigger layout

	// Activate an approval prompt.
	ch := make(chan guard.ApprovalDecision, 1)
	m.Update(ApprovalReqMsg{
		Req:   guard.ApprovalRequest{SessionID: "main", Tool: "shell", Input: json.RawMessage(`{"command":"ls"}`)},
		Reply: ch,
	})

	// PgUp should scroll the timeline, not be ignored.
	m.Update(tea.KeyMsg{Type: tea.KeyPgUp})

	if m.Chat().IsAutoScroll() {
		t.Error("expected autoScroll to be false after PgUp during approval")
	}
}

func TestCountLines_MatchesRender(t *testing.T) {
	tl := []TimelineEntry{
		{Kind: KindUser, Text: "hello world"},
		{Kind: KindAssistant, Text: "response text here"},
		{Kind: KindNotice, Text: "a notice"},
	}
	c := &ChatComponent{
		timeline:   &tl,
		autoScroll: true,
		maxHeight:  100,
		cursor:     -1,
	}

	rendered := c.Render(80)
	counted := c.countLines(80)
	if counted != len(rendered) {
		t.Errorf("countLines=%d, len(Render)=%d", counted, len(rendered))
	}
}

func TestRender_ReusesCachedEntries(t *testing.T) {
	var tl []TimelineEntry
	for i := 0; i < 100; i++ {
		tl = append(tl, TimelineEntry{Kind: KindNotice, Text: fmt.Sprintf("entry %d", i)})
	}
	c := &ChatComponent{
		timeline:   &tl,
		autoScroll: true,
		maxHeight:  10,
		cursor:     -1,
	}

	rendered := c.Render(80)
	if len(rendered) > 10 {
		t.Errorf("expected at most 10 lines, got %d", len(rendered))
	}

	// Every entry is rendered once (to count lines) and cached; a second
	// frame, including a cursor move, must not re-render any of them.
	first := make([]*string, len(tl))
	for i := range tl {
		if len(tl[i].cachedLines) == 0 {
			t.Fatalf("entry %d has no cached lines", i)
		}
		first[i] = &tl[i].cachedLines[0]
	}
	c.SetCursor(95)
	rendered = c.Render(80)
	for i := range tl {
		if &tl[i].cachedLines[0] != first[i] {
			t.Errorf("entry %d was re-rendered", i)
		}
	}
	found := false
	for _, l := range rendered {
		if strings.HasPrefix(l, "> ") {
			found = true
		}
	}
	if !found {
		t.Error("expected cursor marker on the cursor entry")
	}
}

func TestSpinnerTick_SingleChain(t *testing.T) {
	m := New(nil, "main")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	_, cmd := m.Update(agentEventMsg{SessionID: "main", Ev: agent.Event{Kind: agent.EventSending, SessionID: "main"}})
	if cmd == nil {
		t.Fatal("expected a spinner tick to be scheduled on first event")
	}
	// Further deltas while a tick is in flight must not start another chain.
	for i := 0; i < 5; i++ {
		_, cmd = m.Update(agentEventMsg{SessionID: "main", Ev: agent.Event{
			Kind: agent.EventTokenDelta, SessionID: "main", TextDelta: "x",
		}})
		if cmd != nil {
			t.Fatalf("delta %d: expected no new command while a tick is in flight", i)
		}
	}
	// The tick itself reschedules exactly one follow-up.
	_, cmd = m.Update(spinnerTickMsg{})
	if cmd == nil {
		t.Fatal("expected spinner tick to reschedule itself")
	}
}

func TestRenderThrottle_ReusesFrameUntilFlush(t *testing.T) {
	m := New(nil, "main")
	m.SetRenderThrottleMs(100)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m.Update(agentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind: agent.EventTokenDelta, SessionID: "main", TextDelta: "Hello ",
	}})
	v1 := m.View()
	if !strings.Contains(v1, "Hello") {
		t.Fatalf("first delta should render immediately, got:\n%s", v1)
	}

	m.Update(agentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind: agent.EventTokenDelta, SessionID: "main", TextDelta: "world",
	}})
	if v2 := m.View(); v2 != v1 {
		t.Fatal("throttled delta should reuse the previous frame")
	}

	m.Update(renderThrottleMsg{})
	if v3 := m.View(); !strings.Contains(v3, "Hello world") {
		t.Fatalf("throttle flush should show all text, got:\n%s", v3)
	}
}

func TestRenderThrottle_NonDeltaMessageRendersImmediately(t *testing.T) {
	m := New(nil, "main")
	m.SetRenderThrottleMs(100)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m.Update(agentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind: agent.EventTokenDelta, SessionID: "main", TextDelta: "Hello ",
	}})
	m.View()
	m.Update(agentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind: agent.EventTokenDelta, SessionID: "main", TextDelta: "world",
	}})
	if !m.renderPending {
		t.Fatal("expected second delta to be throttled")
	}

	// A key press arriving while a redraw is deferred must show current text.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if m.renderPending {
		t.Error("expected non-delta message to clear renderPending")
	}
	if v := m.View(); !strings.Contains(v, "Hello world") {
		t.Fatalf("expected fresh frame with all text, got:\n%s", v)
	}

	// The same holds for a non-delta agent event, such as a tool call.
	m.Update(agentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind: agent.EventTokenDelta, SessionID: "main", TextDelta: "!",
	}})
	m.Update(agentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind: agent.EventToolCallDone, SessionID: "main", ToolName: "shell",
		ToolCallID: "t1", InputJSON: `{"command":"ls"}`,
	}})
	if v := m.View(); !strings.Contains(v, "Hello world!") || !strings.Contains(v, "$ ls") {
		t.Fatalf("expected fresh frame after tool event, got:\n%s", v)
	}
}
