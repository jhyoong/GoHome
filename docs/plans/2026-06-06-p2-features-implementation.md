# P2 Features Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement three P2 TUI features: thinking block display (full stack), @-prefix fuzzy file search, and pending message queue with visible preview.

**Architecture:** Each feature is a vertical slice built bottom-up (types -> transport -> agent -> TUI). All three features are independent and can be built in any order, but this plan sequences them to avoid merge conflicts in shared files like `model.go`.

**Tech Stack:** Go 1.25, Bubbletea, goldmark, chroma, lipgloss, fd (external subprocess)

---

## Feature A: Thinking Block Display

### Task 1: Add thinking types to common

**Files:**
- Modify: `gohome/internal/llm/common/types.go:18-21` (BlockKind constants)
- Modify: `gohome/internal/llm/common/types.go:40-47` (EventKind constants)
- Modify: `gohome/internal/llm/common/types.go:56-65` (StreamEvent struct)
- Test: `gohome/internal/llm/common/types_test.go`

**Step 1: Write the failing test**

Add to `gohome/internal/llm/common/types_test.go`:

```go
func TestThinkingBlockKind(t *testing.T) {
	if string(common.BlockThinking) != "thinking" {
		t.Errorf("BlockThinking: got %q, want %q", common.BlockThinking, "thinking")
	}
}

func TestThinkingEventKinds(t *testing.T) {
	if string(common.EventThinkingDelta) != "thinking_delta" {
		t.Errorf("EventThinkingDelta: got %q", common.EventThinkingDelta)
	}
	if string(common.EventThinkingDone) != "thinking_done" {
		t.Errorf("EventThinkingDone: got %q", common.EventThinkingDone)
	}
}

func TestStreamEvent_ThinkingDelta(t *testing.T) {
	ev := common.StreamEvent{Kind: common.EventThinkingDelta, ThinkingDelta: "reasoning about X"}
	if ev.ThinkingDelta != "reasoning about X" {
		t.Errorf("ThinkingDelta: got %q", ev.ThinkingDelta)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd gohome && go test ./internal/llm/common/ -run 'TestThinking' -v`
Expected: FAIL -- `BlockThinking`, `EventThinkingDelta`, `EventThinkingDone`, `ThinkingDelta` undefined

**Step 3: Write minimal implementation**

In `gohome/internal/llm/common/types.go`, add:

```go
// After BlockToolResult:
BlockThinking BlockKind = "thinking"

// After EventError:
EventThinkingDelta EventKind = "thinking_delta"
EventThinkingDone  EventKind = "thinking_done"
```

Add field to `StreamEvent`:

```go
type StreamEvent struct {
	Kind          EventKind
	TextDelta     string
	ThinkingDelta string
	ToolCallID    string
	ToolName      string
	InputJSON     string
	StopReason    string
	Usage         *Usage
	Err           error
}
```

**Step 4: Run test to verify it passes**

Run: `cd gohome && go test ./internal/llm/common/ -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add gohome/internal/llm/common/types.go gohome/internal/llm/common/types_test.go
git commit -m "feat(common): add BlockThinking, EventThinkingDelta, EventThinkingDone types"
```

---

### Task 2: Parse thinking blocks in Anthropic SSE translator

**Files:**
- Modify: `gohome/internal/llm/anthropic/translate.go:37-56` (contentBlockStartData, contentBlockDeltaData structs)
- Modify: `gohome/internal/llm/anthropic/translate.go:68-201` (translateEvents function)
- Test: `gohome/internal/llm/anthropic/translate_test.go`

**Step 1: Write the failing test**

Add to `gohome/internal/llm/anthropic/translate_test.go`:

```go
func TestTranslateEvents_ThinkingBlock(t *testing.T) {
	frames := []sseFrame{
		{event: "content_block_start", data: `{"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}`},
		{event: "content_block_delta", data: `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me reason about this"}}`},
		{event: "content_block_delta", data: `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":" step by step."}}`},
		{event: "content_block_stop", data: `{"type":"content_block_stop","index":0}`},
		{event: "content_block_start", data: `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`},
		{event: "content_block_delta", data: `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"The answer is 42."}}`},
		{event: "content_block_stop", data: `{"type":"content_block_stop","index":1}`},
		{event: "message_delta", data: `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":20}}`},
		{event: "message_stop", data: `{"type":"message_stop"}`},
	}

	events := collectEvents(translateEvents(context.Background(), makeFrames(frames)))

	var thinkingDeltas, textDeltas, thinkingDones, turnDones int
	var thinkingText, textText string
	for _, e := range events {
		switch e.Kind {
		case common.EventThinkingDelta:
			thinkingDeltas++
			thinkingText += e.ThinkingDelta
		case common.EventThinkingDone:
			thinkingDones++
		case common.EventTextDelta:
			textDeltas++
			textText += e.TextDelta
		case common.EventTurnDone:
			turnDones++
		}
	}

	if thinkingDeltas != 2 {
		t.Errorf("expected 2 thinking deltas, got %d", thinkingDeltas)
	}
	if thinkingText != "Let me reason about this step by step." {
		t.Errorf("thinking text: got %q", thinkingText)
	}
	if thinkingDones != 1 {
		t.Errorf("expected 1 thinking done, got %d", thinkingDones)
	}
	if textDeltas != 1 {
		t.Errorf("expected 1 text delta, got %d", textDeltas)
	}
	if textText != "The answer is 42." {
		t.Errorf("text: got %q", textText)
	}
	if turnDones != 1 {
		t.Errorf("expected 1 turn done, got %d", turnDones)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd gohome && go test ./internal/llm/anthropic/ -run TestTranslateEvents_ThinkingBlock -v`
Expected: FAIL -- thinking deltas are not emitted (block type "thinking" is not handled, content_block_delta with `thinking_delta` type falls through)

**Step 3: Write minimal implementation**

In `gohome/internal/llm/anthropic/translate.go`:

1. Add `Thinking` field to `contentBlockDeltaData.Delta`:

```go
type contentBlockDeltaData struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		Thinking    string `json:"thinking"`
	} `json:"delta"`
}
```

2. In `translateEvents`, in the `content_block_start` case, the existing code already stores the block type in `blockTypes[d.Index]`. The `"thinking"` type will be stored automatically.

3. In the `content_block_delta` case, add a new case for `"thinking_delta"`:

```go
case "thinking_delta":
	if !send(common.StreamEvent{
		Kind:          common.EventThinkingDelta,
		ThinkingDelta: d.Delta.Thinking,
	}) {
		return
	}
```

4. In the `content_block_stop` case, add handling for `"thinking"` blocks:

```go
if blockTypes[raw.Index] == "thinking" {
	if !send(common.StreamEvent{
		Kind: common.EventThinkingDone,
	}) {
		return
	}
}
```

Add this before the existing `tool_use` check. The full `content_block_stop` section becomes:

```go
case "content_block_stop":
	var raw struct {
		Index int `json:"index"`
	}
	if err := json.Unmarshal([]byte(frame.data), &raw); err != nil {
		send(common.StreamEvent{Kind: common.EventError, Err: err})
		return
	}
	if blockTypes[raw.Index] == "thinking" {
		if !send(common.StreamEvent{
			Kind: common.EventThinkingDone,
		}) {
			return
		}
	} else if blockTypes[raw.Index] == "tool_use" {
		tb := toolBlocks[raw.Index]
		if tb != nil {
			if !send(common.StreamEvent{
				Kind:       common.EventToolCallDone,
				ToolCallID: tb.id,
				ToolName:   tb.name,
				InputJSON:  string(tb.inputBuf),
			}) {
				return
			}
		}
		delete(toolBlocks, raw.Index)
	}
	delete(blockTypes, raw.Index)
```

**Step 4: Run test to verify it passes**

Run: `cd gohome && go test ./internal/llm/anthropic/ -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add gohome/internal/llm/anthropic/translate.go gohome/internal/llm/anthropic/translate_test.go
git commit -m "feat(anthropic): parse thinking blocks from SSE stream"
```

---

### Task 3: Add thinking events to agent layer

**Files:**
- Modify: `gohome/internal/agent/events.go:13-23` (EventKind constants)
- Modify: `gohome/internal/agent/events.go:32-44` (Event struct)
- Modify: `gohome/internal/agent/turn.go:38-99` (Turn event loop)
- Test: `gohome/internal/agent/events_test.go`
- Test: `gohome/internal/agent/turn_test.go`

**Step 1: Write the failing tests**

Add to `gohome/internal/agent/events_test.go`, inside `TestEventKindConstants`:

```go
{EventThinkingDelta, "thinking_delta"},
{EventThinkingDone, "thinking_done"},
```

Add to `gohome/internal/agent/turn_test.go`:

```go
func TestTurn_ThinkingThenText(t *testing.T) {
	events := []common.StreamEvent{
		{Kind: common.EventThinkingDelta, ThinkingDelta: "reasoning..."},
		{Kind: common.EventThinkingDone},
		{Kind: common.EventTextDelta, TextDelta: "The answer"},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	stopReason, err := a.Turn(context.Background(), sess)
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if stopReason != "end_turn" {
		t.Errorf("stopReason: got %q", stopReason)
	}

	// Frontend should have received thinking_delta, thinking_done, token_delta, usage, turn_done.
	wantKinds := []EventKind{EventThinkingDelta, EventThinkingDone, EventTokenDelta, EventUsageUpdated, EventTurnDone}
	if len(fe.events) != len(wantKinds) {
		t.Fatalf("frontend events: got %d, want %d\nevents: %+v", len(fe.events), len(wantKinds), fe.events)
	}
	for i, wk := range wantKinds {
		if fe.events[i].Kind != wk {
			t.Errorf("fe.events[%d].Kind: got %v, want %v", i, fe.events[i].Kind, wk)
		}
	}
	if fe.events[0].ThinkingDelta != "reasoning..." {
		t.Errorf("thinking delta: got %q", fe.events[0].ThinkingDelta)
	}

	// History should have one assistant message with text only (thinking is not persisted).
	if len(sess.History) != 1 {
		t.Fatalf("history: got %d", len(sess.History))
	}
	if sess.History[0].Content[0].Text != "The answer" {
		t.Errorf("text: got %q", sess.History[0].Content[0].Text)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd gohome && go test ./internal/agent/ -run 'TestEventKindConstants|TestTurn_ThinkingThenText' -v`
Expected: FAIL -- `EventThinkingDelta`, `EventThinkingDone`, `ThinkingDelta` undefined

**Step 3: Write minimal implementation**

In `gohome/internal/agent/events.go`, add constants:

```go
EventThinkingDelta EventKind = "thinking_delta"
EventThinkingDone  EventKind = "thinking_done"
```

Add field to `Event`:

```go
ThinkingDelta string
```

In `gohome/internal/agent/turn.go`, add two cases to the switch inside the event loop (after the `EventTextDelta` case):

```go
case common.EventThinkingDelta:
	a.Frontend.Emit(sess.ID, Event{
		Kind:          EventThinkingDelta,
		SessionID:     sess.ID,
		ThinkingDelta: ev.ThinkingDelta,
	})

case common.EventThinkingDone:
	a.Frontend.Emit(sess.ID, Event{
		Kind:      EventThinkingDone,
		SessionID: sess.ID,
	})
```

Note: thinking text is NOT accumulated into `textBuf` or `blocks` -- it is forwarded to the frontend for display only and not persisted in session history.

**Step 4: Run tests to verify they pass**

Run: `cd gohome && go test ./internal/agent/ -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add gohome/internal/agent/events.go gohome/internal/agent/turn.go gohome/internal/agent/events_test.go gohome/internal/agent/turn_test.go
git commit -m "feat(agent): forward thinking block events to frontend"
```

---

### Task 4: Render thinking blocks in TUI

**Files:**
- Modify: `gohome/internal/tui/model.go:203-307` (handleAgentEvent)
- Modify: `gohome/internal/tui/chat.go:64-153` (Render, add thinking case)
- Test: `gohome/internal/tui/chat_test.go`
- Test: `gohome/internal/tui/tui_test.go`

**Step 1: Write the failing tests**

Add to `gohome/internal/tui/chat_test.go`:

```go
func TestChatRenderThinkingCollapsed(t *testing.T) {
	entries := []TimelineEntry{{Kind: "thinking", Text: "Let me reason\nabout this\nstep by step."}}
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
	entries := []TimelineEntry{{Kind: "thinking", Text: "Step 1: analyze\nStep 2: solve", Expanded: true}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "Step 1") {
		t.Errorf("expanded thinking content missing: %q", joined)
	}
}
```

Add to `gohome/internal/tui/tui_test.go`:

```go
func TestAgentEventThinkingDelta(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() {
		_ = tm.Quit()
	})

	tm.Send(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:          agent.EventThinkingDelta,
		SessionID:     "main",
		ThinkingDelta: "reasoning about the problem",
	}})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Thinking..."))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
```

**Step 2: Run tests to verify they fail**

Run: `cd gohome && go test ./internal/tui/ -run 'TestChatRenderThinking|TestAgentEventThinkingDelta' -v`
Expected: FAIL -- `"thinking"` case not handled in chat Render; `EventThinkingDelta` not handled in handleAgentEvent

**Step 3: Write minimal implementation**

In `gohome/internal/tui/model.go`, add to `handleAgentEvent` switch (before the `EventToolCallDone` case):

```go
case agent.EventThinkingDelta:
	n := len(sv.Timeline)
	if n > 0 && sv.Timeline[n-1].Kind == "thinking" {
		sv.Timeline[n-1].Text += ev.ThinkingDelta
	} else {
		sv.Timeline = append(sv.Timeline, TimelineEntry{
			Kind: "thinking",
			Text: ev.ThinkingDelta,
		})
	}

case agent.EventThinkingDone:
	// No-op: the thinking entry is already complete.
```

Also update the spinner section at the bottom of `handleAgentEvent` to start on thinking deltas. Change:

```go
case agent.EventTokenDelta:
	if !m.spinner.Active() {
		m.spinner.Start("Thinking...")
	}
```

To:

```go
case agent.EventThinkingDelta:
	if !m.spinner.Active() {
		m.spinner.Start("Thinking...")
	}
case agent.EventTokenDelta:
	if !m.spinner.Active() {
		m.spinner.Start("Generating...")
	} else {
		m.spinner.SetMessage("Generating...")
	}
```

In `gohome/internal/tui/chat.go`, add a `"thinking"` case inside the `Render` method's switch, after the `"assistant"` case:

```go
case "thinking":
	if e.Expanded {
		mdLines := RenderMarkdown(e.Text, maxWidth-4)
		if len(mdLines) == 0 {
			mdLines = WrapText(e.Text, maxWidth-4)
		}
		entryLines = append(entryLines, marker+ansiDim+ansiItalic+"Thinking..."+ansiReset)
		for _, l := range mdLines {
			entryLines = append(entryLines, "    "+ansiDim+ansiItalic+l+ansiReset)
		}
	} else {
		label := "Thinking..."
		lines := strings.Split(strings.TrimSpace(e.Text), "\n")
		if len(lines) > 1 {
			label = fmt.Sprintf("Thinking... (%d lines)", len(lines))
		}
		entryLines = append(entryLines, marker+ansiDim+ansiItalic+label+ansiReset)
	}
```

This requires adding `"fmt"` and `"strings"` to the chat.go imports (if not already present) and referencing the ANSI constants from `markdown.go`. Since these constants are package-level in the `tui` package, they are accessible directly.

**Step 4: Run tests to verify they pass**

Run: `cd gohome && go test ./internal/tui/ -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add gohome/internal/tui/model.go gohome/internal/tui/chat.go gohome/internal/tui/chat_test.go gohome/internal/tui/tui_test.go
git commit -m "feat(tui): render collapsible thinking blocks in chat timeline"
```

---

## Feature B: @-Prefix Fuzzy File Search

### Task 5: Implement file search scoring

**Files:**
- Create: `gohome/internal/tui/filesearch.go`
- Test: `gohome/internal/tui/filesearch_test.go`

**Step 1: Write the failing test**

Create `gohome/internal/tui/filesearch_test.go`:

```go
package tui

import (
	"testing"
)

func TestScoreResults_ExactFilename(t *testing.T) {
	results := scoreResults("main.go", []string{"cmd/main.go", "internal/tui/model.go", "main.go"})
	if len(results) == 0 {
		t.Fatal("no results")
	}
	if results[0].path != "main.go" {
		t.Errorf("best match: got %q, want %q", results[0].path, "main.go")
	}
}

func TestScoreResults_StartsWithFilename(t *testing.T) {
	results := scoreResults("mod", []string{"internal/tui/model.go", "go.mod", "modfile/x.go"})
	if len(results) < 2 {
		t.Fatal("too few results")
	}
	// "go.mod" starts with "mod" in filename -- should rank above substring-in-path
	// But "modfile/x.go" has "mod" at start of a directory name, not filename.
	// The file whose basename starts with "mod" should come first.
}

func TestScoreResults_SubstringInFilename(t *testing.T) {
	results := scoreResults("odel", []string{"internal/tui/model.go", "cmd/main.go"})
	if len(results) == 0 {
		t.Fatal("no results")
	}
	if results[0].path != "internal/tui/model.go" {
		t.Errorf("expected model.go first, got %q", results[0].path)
	}
}

func TestScoreResults_ShorterPathBreaksTie(t *testing.T) {
	results := scoreResults("main.go", []string{"a/b/c/main.go", "a/main.go"})
	if len(results) < 2 {
		t.Fatal("too few results")
	}
	if results[0].path != "a/main.go" {
		t.Errorf("shorter path should win tie: got %q", results[0].path)
	}
}

func TestScoreResults_EmptyQuery(t *testing.T) {
	results := scoreResults("", []string{"a.go", "b.go"})
	if len(results) != 0 {
		t.Errorf("empty query should return no results, got %d", len(results))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd gohome && go test ./internal/tui/ -run TestScoreResults -v`
Expected: FAIL -- `scoreResults` undefined

**Step 3: Write minimal implementation**

Create `gohome/internal/tui/filesearch.go`:

```go
package tui

import (
	"path/filepath"
	"sort"
	"strings"
)

type scoredResult struct {
	path  string
	score int
}

func scoreResults(query string, paths []string) []scoredResult {
	if query == "" {
		return nil
	}
	q := strings.ToLower(query)
	var results []scoredResult
	for _, p := range paths {
		base := strings.ToLower(filepath.Base(p))
		lp := strings.ToLower(p)
		var score int
		switch {
		case base == q:
			score = 0
		case strings.HasPrefix(base, q):
			score = 20
		case strings.Contains(base, q):
			score = 50
		case strings.Contains(lp, q):
			score = 70
		default:
			continue
		}
		results = append(results, scoredResult{path: p, score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score < results[j].score
		}
		return len(results[i].path) < len(results[j].path)
	})
	return results
}
```

**Step 4: Run test to verify it passes**

Run: `cd gohome && go test ./internal/tui/ -run TestScoreResults -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add gohome/internal/tui/filesearch.go gohome/internal/tui/filesearch_test.go
git commit -m "feat(tui): add file search scoring and ranking"
```

---

### Task 6: Implement fd subprocess and FileSearchPopup component

**Files:**
- Modify: `gohome/internal/tui/filesearch.go`
- Test: `gohome/internal/tui/filesearch_test.go`

**Step 1: Write the failing tests**

Add to `gohome/internal/tui/filesearch_test.go`:

```go
func TestFileSearchPopup_Render_Empty(t *testing.T) {
	p := NewFileSearchPopup()
	lines := p.Render(80)
	if len(lines) != 0 {
		t.Errorf("empty popup should render 0 lines, got %d", len(lines))
	}
}

func TestFileSearchPopup_Render_WithResults(t *testing.T) {
	p := NewFileSearchPopup()
	p.results = []scoredResult{
		{path: "src/main.go", score: 0},
		{path: "src/util.go", score: 20},
		{path: "test/main_test.go", score: 50},
	}
	p.visible = true
	lines := p.Render(80)
	if len(lines) == 0 {
		t.Fatal("expected non-empty render")
	}
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "src/main.go") {
		t.Errorf("first result missing: %q", joined)
	}
}

func TestFileSearchPopup_SelectionWrap(t *testing.T) {
	p := NewFileSearchPopup()
	p.results = []scoredResult{
		{path: "a.go", score: 0},
		{path: "b.go", score: 20},
	}
	p.visible = true
	p.MoveDown()
	if p.selected != 1 {
		t.Errorf("after MoveDown: selected=%d, want 1", p.selected)
	}
	p.MoveDown()
	if p.selected != 0 {
		t.Errorf("after second MoveDown (wrap): selected=%d, want 0", p.selected)
	}
}

func TestFileSearchPopup_SelectedPath(t *testing.T) {
	p := NewFileSearchPopup()
	p.results = []scoredResult{
		{path: "a.go", score: 0},
		{path: "b.go", score: 20},
	}
	p.visible = true
	p.selected = 1
	got := p.SelectedPath()
	if got != "b.go" {
		t.Errorf("SelectedPath: got %q, want %q", got, "b.go")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd gohome && go test ./internal/tui/ -run 'TestFileSearchPopup' -v`
Expected: FAIL -- `NewFileSearchPopup`, `MoveDown`, `SelectedPath` undefined

**Step 3: Write minimal implementation**

Add to `gohome/internal/tui/filesearch.go`:

```go
import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	maxFileResults  = 50
	maxVisibleFiles = 10
	searchDebounce  = 20 * time.Millisecond
)

var fileSearchBorder = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

type fileSearchResultMsg struct {
	Query   string
	Results []scoredResult
}

type FileSearchPopup struct {
	query    string
	results  []scoredResult
	selected int
	visible  bool
	cancel   context.CancelFunc
}

func NewFileSearchPopup() *FileSearchPopup {
	return &FileSearchPopup{}
}

func (p *FileSearchPopup) MoveDown() {
	if len(p.results) == 0 {
		return
	}
	p.selected = (p.selected + 1) % len(p.results)
}

func (p *FileSearchPopup) MoveUp() {
	if len(p.results) == 0 {
		return
	}
	p.selected--
	if p.selected < 0 {
		p.selected = len(p.results) - 1
	}
}

func (p *FileSearchPopup) SelectedPath() string {
	if len(p.results) == 0 || p.selected >= len(p.results) {
		return ""
	}
	return p.results[p.selected].path
}

func (p *FileSearchPopup) SetResults(query string, results []scoredResult) {
	if query != p.query {
		return // stale
	}
	p.results = results
	p.selected = 0
	p.visible = len(results) > 0
}

func (p *FileSearchPopup) Hide() {
	p.visible = false
	p.results = nil
	p.selected = 0
	p.query = ""
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
}

func (p *FileSearchPopup) Render(width int) []string {
	if !p.visible || len(p.results) == 0 {
		return nil
	}

	visible := p.results
	if len(visible) > maxVisibleFiles {
		visible = visible[:maxVisibleFiles]
	}

	topBorder := fileSearchBorder.Render(strings.Repeat("─", width))
	var lines []string
	lines = append(lines, topBorder)
	for i, r := range visible {
		prefix := "  "
		if i == p.selected {
			prefix = "> "
		}
		line := prefix + r.path
		if VisualWidth(line) > width {
			line = TruncateText(line, width)
		}
		if i == p.selected {
			line = "\x1b[7m" + line + "\x1b[0m"
		}
		lines = append(lines, line)
	}
	if len(p.results) > maxVisibleFiles {
		lines = append(lines, fmt.Sprintf("  ... %d more", len(p.results)-maxVisibleFiles))
	}
	botBorder := fileSearchBorder.Render(strings.Repeat("─", width))
	lines = append(lines, botBorder)
	return lines
}

func searchFilesCmd(query string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		var cmd *exec.Cmd
		if _, err := exec.LookPath("fd"); err == nil {
			cmd = exec.CommandContext(ctx, "fd", "--type", "f", "--color", "never", query)
		} else {
			cmd = exec.CommandContext(ctx, "find", ".", "-type", "f", "-name", "*"+query+"*")
		}

		out, err := cmd.Output()
		if err != nil {
			return fileSearchResultMsg{Query: query, Results: nil}
		}

		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) == 1 && lines[0] == "" {
			return fileSearchResultMsg{Query: query, Results: nil}
		}
		if len(lines) > maxFileResults {
			lines = lines[:maxFileResults]
		}

		results := scoreResults(query, lines)
		return fileSearchResultMsg{Query: query, Results: results}
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `cd gohome && go test ./internal/tui/ -run 'TestFileSearchPopup|TestScoreResults' -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add gohome/internal/tui/filesearch.go gohome/internal/tui/filesearch_test.go
git commit -m "feat(tui): add FileSearchPopup component and fd subprocess"
```

---

### Task 7: Integrate file search into Model

**Files:**
- Modify: `gohome/internal/tui/model.go` (add fields, Update logic, View rendering)
- Test: `gohome/internal/tui/tui_test.go`

**Step 1: Write the failing test**

Add to `gohome/internal/tui/tui_test.go`:

```go
func TestFileSearchTriggersOnAtSign(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() {
		_ = tm.Quit()
	})

	tm.Type("@mod")

	// After typing @mod the file search should be active.
	// We can't easily test fd results in unit tests, but we can verify
	// the editor contains "@mod".
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("@mod"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
```

**Step 2: Run test to verify it fails**

Run: `cd gohome && go test ./internal/tui/ -run TestFileSearchTriggersOnAtSign -v`
Expected: This test should actually pass already since we're just typing into the editor. The real integration is in the key routing. For a more meaningful test, we test the plumbing by sending a `fileSearchResultMsg` directly.

Replace the test with:

```go
func TestFileSearchResultMsgShowsPopup(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() {
		_ = tm.Quit()
	})

	// Type @mod to activate search mode.
	tm.Type("@mod")

	// Simulate fd results arriving.
	tm.Send(tui.FileSearchResultMsg{
		Query:   "mod",
		Results: []tui.ScoredResult{{Path: "go.mod", Score: 0}, {Path: "internal/tui/model.go", Score: 50}},
	})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("go.mod"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
```

Note: this requires exporting `FileSearchResultMsg` and `ScoredResult` for the `tui_test` package. Rename:
- `fileSearchResultMsg` -> `FileSearchResultMsg` (exported)
- `scoredResult` -> `ScoredResult` with exported fields `Path` and `Score`

**Step 3: Write the implementation**

This is the integration step. Changes to `model.go`:

1. Add fields:

```go
fileSearch    *FileSearchPopup
fileSearching bool
```

2. Initialize in `New()`:

```go
fileSearch: NewFileSearchPopup(),
```

3. In `Update()`, handle `FileSearchResultMsg`:

```go
case FileSearchResultMsg:
	if m.fileSearching {
		m.fileSearch.SetResults(msg.Query, msg.Results)
	}
```

4. After forwarding keystrokes to `editor.HandleInput()` in the default case, detect `@` query:

```go
default:
	cmd := m.editor.HandleInput(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	// Check for @-prefix file search.
	if q, ok := m.extractAtQuery(); ok && q != m.fileSearch.query {
		m.fileSearching = true
		m.fileSearch.query = q
		m.fileSearch.selected = 0
		cmds = append(cmds, searchFilesCmd(q))
	} else if !ok && m.fileSearching {
		m.fileSearching = false
		m.fileSearch.Hide()
	}
```

5. When `fileSearching` is true, intercept Up/Down/Enter/Esc before they reach the editor. Add this block before the `default` case in the key switch:

```go
case tea.KeyUp:
	if m.fileSearching && m.fileSearch.visible {
		m.fileSearch.MoveUp()
		return m, nil
	}
	// ... existing PgUp handling or fall through to default
case tea.KeyDown:
	if m.fileSearching && m.fileSearch.visible {
		m.fileSearch.MoveDown()
		return m, nil
	}
case tea.KeyEsc:
	if m.fileSearching {
		m.fileSearching = false
		m.fileSearch.Hide()
		return m, nil
	}
```

For Enter when `fileSearching` and the popup is visible, insert the selected path replacing the `@query`:

```go
// Inside the Enter handling, before the existing empty-text check:
if m.fileSearching && m.fileSearch.visible {
	path := m.fileSearch.SelectedPath()
	if path != "" {
		m.replaceAtQuery(path)
	}
	m.fileSearching = false
	m.fileSearch.Hide()
	return m, tea.Batch(cmds...)
}
```

6. Add helper methods:

```go
func (m *Model) extractAtQuery() (string, bool) {
	val := m.editor.Value()
	// Find the last @ that is either at position 0 or preceded by whitespace.
	idx := strings.LastIndex(val, "@")
	if idx < 0 {
		return "", false
	}
	if idx > 0 && val[idx-1] != ' ' && val[idx-1] != '\n' && val[idx-1] != '\t' {
		return "", false
	}
	query := val[idx+1:]
	// No query yet or query contains spaces (end of @-search context).
	if strings.ContainsAny(query, " \t\n") {
		return "", false
	}
	return query, true
}

func (m *Model) replaceAtQuery(replacement string) {
	val := m.editor.Value()
	idx := strings.LastIndex(val, "@")
	if idx < 0 {
		return
	}
	newVal := val[:idx] + replacement
	m.editor.SetValue(newVal)
}
```

7. In `View()`, render the file search popup between the chat and the editor when active:

```go
// After chat lines, before spinner:
if m.fileSearching {
	popupLines := m.fileSearch.Render(m.winW)
	if len(popupLines) > 0 {
		sections = append(sections, strings.Join(popupLines, "\n"))
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `cd gohome && go test ./internal/tui/ -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add gohome/internal/tui/model.go gohome/internal/tui/filesearch.go gohome/internal/tui/filesearch_test.go gohome/internal/tui/tui_test.go
git commit -m "feat(tui): integrate @-prefix fuzzy file search into editor"
```

---

## Feature C: Pending Message Queue

### Task 8: Add PendingMessagesComponent

**Files:**
- Create: `gohome/internal/tui/pending.go`
- Test: `gohome/internal/tui/pending_test.go`

**Step 1: Write the failing test**

Create `gohome/internal/tui/pending_test.go`:

```go
package tui

import (
	"strings"
	"testing"
)

func TestPendingMessages_RenderEmpty(t *testing.T) {
	msgs := []string{}
	c := NewPendingMessages(&msgs)
	lines := c.Render(80)
	if len(lines) != 0 {
		t.Errorf("empty queue should render 0 lines, got %d", len(lines))
	}
}

func TestPendingMessages_RenderWithMessages(t *testing.T) {
	msgs := []string{"fix the tests", "update the README"}
	c := NewPendingMessages(&msgs)
	lines := c.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "Queued:") {
		t.Errorf("header missing: %q", joined)
	}
	if !strings.Contains(joined, "[1]") {
		t.Errorf("[1] marker missing: %q", joined)
	}
	if !strings.Contains(joined, "fix the tests") {
		t.Errorf("first message missing: %q", joined)
	}
	if !strings.Contains(joined, "[2]") {
		t.Errorf("[2] marker missing: %q", joined)
	}
	if !strings.Contains(joined, "update the README") {
		t.Errorf("second message missing: %q", joined)
	}
}

func TestPendingMessages_TruncatesLongMessages(t *testing.T) {
	long := strings.Repeat("x", 200)
	msgs := []string{long}
	c := NewPendingMessages(&msgs)
	lines := c.Render(80)
	for _, l := range lines {
		if VisualWidth(StripAnsi(l)) > 80 {
			t.Errorf("line exceeds width: %d cols", VisualWidth(StripAnsi(l)))
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd gohome && go test ./internal/tui/ -run TestPendingMessages -v`
Expected: FAIL -- `NewPendingMessages` undefined

**Step 3: Write minimal implementation**

Create `gohome/internal/tui/pending.go`:

```go
package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var pendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)

type PendingMessagesComponent struct {
	messages *[]string
}

func NewPendingMessages(messages *[]string) *PendingMessagesComponent {
	return &PendingMessagesComponent{messages: messages}
}

func (p *PendingMessagesComponent) Render(width int) []string {
	if p.messages == nil || len(*p.messages) == 0 {
		return nil
	}
	var lines []string
	lines = append(lines, pendingStyle.Render("Queued:"))
	for i, msg := range *p.messages {
		prefix := fmt.Sprintf("  [%d] ", i+1)
		maxW := width - VisualWidth(prefix)
		if maxW < 10 {
			maxW = 10
		}
		text := msg
		if VisualWidth(text) > maxW {
			text = TruncateText(text, maxW-3) + "..."
		}
		lines = append(lines, pendingStyle.Render(prefix+text))
	}
	return lines
}
```

**Step 4: Run test to verify it passes**

Run: `cd gohome && go test ./internal/tui/ -run TestPendingMessages -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add gohome/internal/tui/pending.go gohome/internal/tui/pending_test.go
git commit -m "feat(tui): add PendingMessagesComponent for message queue display"
```

---

### Task 9: Integrate pending message queue into Model

**Files:**
- Modify: `gohome/internal/tui/model.go` (add field, queue logic in Update, dequeue in handleAgentEvent, render in View, /cancel clears queue)
- Test: `gohome/internal/tui/tui_test.go`

**Step 1: Write the failing tests**

Add to `gohome/internal/tui/tui_test.go`:

```go
func TestPendingQueue_EnterWhileStreaming(t *testing.T) {
	fe := tui.NewFrontend()
	m := tui.New(fe, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() {
		_ = tm.Quit()
	})

	// Start a streaming session by sending a token delta (sets InFlight).
	tm.Send(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventTokenDelta,
		SessionID: "main",
		TextDelta: "working on it...",
	}})

	// Type and submit while streaming.
	tm.Type("fix the tests")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// The message should appear in the pending queue, not be sent.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Queued:")) && bytes.Contains(out, []byte("fix the tests"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

func TestPendingQueue_DequeueOnTurnDone(t *testing.T) {
	fe := tui.NewFrontend()
	m := tui.New(fe, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() {
		_ = tm.Quit()
	})

	// Read input channel in background.
	received := make(chan string, 2)
	go func() {
		for s := range fe.InputCh() {
			received <- s
		}
	}()

	// Start streaming.
	tm.Send(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventTokenDelta,
		SessionID: "main",
		TextDelta: "response",
	}})

	// Queue a message.
	tm.Type("queued msg")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// End the turn -- should auto-dequeue.
	tm.Send(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventTurnDone,
		SessionID: "main",
	}})

	// The dequeued message should arrive on the input channel.
	select {
	case got := <-received:
		if got != "queued msg" {
			t.Errorf("dequeued message: got %q, want %q", got, "queued msg")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for dequeued message")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd gohome && go test ./internal/tui/ -run 'TestPendingQueue' -v`
Expected: FAIL -- no pending queue logic exists yet; messages go directly to the input channel even while streaming.

**Step 3: Write the implementation**

Changes to `gohome/internal/tui/model.go`:

1. Add fields to `Model`:

```go
pendingMessages []string
pending         *PendingMessagesComponent
```

2. Initialize in `New()`:

```go
m.pending = NewPendingMessages(&m.pendingMessages)
```

3. Modify the Enter handling (non-slash, non-empty text). Change the block that currently does:

```go
} else {
	sv := m.getOrCreateSession(m.focused, 0)
	sv.Timeline = append(sv.Timeline, TimelineEntry{
		Kind: "user",
		Text: text,
	})
	sv.InFlight = true
	m.editor.SetValue("")
	m.cursor = len(sv.Timeline) - 1
	m.rebuildViewport()
	cmds = append(cmds, m.sendInputCmd(text))
}
```

To:

```go
} else {
	sv := m.getOrCreateSession(m.focused, 0)
	if sv.InFlight {
		// Queue the message instead of sending it.
		if len(m.pendingMessages) >= 10 {
			m.statusMsg = "Message queue full (10)"
		} else {
			m.pendingMessages = append(m.pendingMessages, text)
			m.editor.SetValue("")
		}
	} else {
		sv.Timeline = append(sv.Timeline, TimelineEntry{
			Kind: "user",
			Text: text,
		})
		sv.InFlight = true
		m.editor.SetValue("")
		m.cursor = len(sv.Timeline) - 1
		m.rebuildViewport()
		cmds = append(cmds, m.sendInputCmd(text))
	}
}
```

4. In `handleAgentEvent`, modify the `EventTurnDone` case to auto-dequeue:

```go
case agent.EventTurnDone:
	sv.InFlight = false
	// Auto-dequeue pending messages.
	if msg.SessionID == m.focused && len(m.pendingMessages) > 0 {
		text := m.pendingMessages[0]
		m.pendingMessages = m.pendingMessages[1:]
		sv.Timeline = append(sv.Timeline, TimelineEntry{
			Kind: "user",
			Text: text,
		})
		sv.InFlight = true
		m.cursor = len(sv.Timeline) - 1
		return m.sendInputCmd(text)
	}
```

Note: `handleAgentEvent` already returns a `tea.Cmd`. The `EventTurnDone` case currently just sets `sv.InFlight = false` and falls through. Now it conditionally returns a cmd.

5. Modify `/cancel` to clear the queue. In `handleSlashCommand`, the `/cancel` case:

```go
case "/cancel":
	if m.slashCB.CancelSession != nil {
		m.slashCB.CancelSession(m.focused)
	}
	sv := m.getOrCreateSession(m.focused, 0)
	sv.InFlight = false
	sv.Timeline = append(sv.Timeline, TimelineEntry{Kind: "notice", Text: "Cancelled."})
	m.pendingMessages = m.pendingMessages[:0]
	m.statusMsg = "Cancelled"
```

6. Add Ctrl+D to remove last queued message. In the key handling, add a case:

```go
case tea.KeyCtrlD:
	if strings.TrimSpace(m.editor.Value()) == "" && len(m.pendingMessages) > 0 {
		m.pendingMessages = m.pendingMessages[:len(m.pendingMessages)-1]
		m.statusMsg = fmt.Sprintf("Removed queued message (%d remaining)", len(m.pendingMessages))
		return m, nil
	}
```

7. In `View()`, render pending messages between spinner and status message:

```go
// After spinner lines:
pendingLines := m.pending.Render(m.winW)
if len(pendingLines) > 0 {
	sections = append(sections, strings.Join(pendingLines, "\n"))
}
```

**Step 4: Run tests to verify they pass**

Run: `cd gohome && go test ./internal/tui/ -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add gohome/internal/tui/model.go gohome/internal/tui/pending.go gohome/internal/tui/tui_test.go
git commit -m "feat(tui): integrate pending message queue with auto-dequeue on turn end"
```

---

### Task 10: Run full test suite and verify

**Step 1: Run all tests**

Run: `cd gohome && go test ./... -v`
Expected: ALL PASS

**Step 2: Run go vet**

Run: `cd gohome && go vet ./...`
Expected: No issues

**Step 3: Final commit (if any fixups were needed)**

If any tests needed fixes, commit them:

```bash
git add -A
git commit -m "fix: address test issues from P2 feature integration"
```
