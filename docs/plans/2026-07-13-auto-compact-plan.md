# Auto-Compact Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Automatically compact conversation history via LLM-based summarization when token usage approaches the context window limit, with configurable percentage-based or fixed-leftover trigger modes.

**Architecture:** Compaction logic lives in `agent/compact.go`. A check runs in `Agent.Run()` after each `Turn()` completes. When triggered, it makes a summarization LLM call and replaces `sess.History` with the summary. The JSONL writer records compaction events and `session.Load` handles them on resume.

**Tech Stack:** Go 1.25, Bubble Tea TUI, JSONL session persistence

---

### Task 1: Add config fields and defaults

**Files:**
- Modify: `gohome/internal/config/defaults.go:1-16`
- Modify: `gohome/internal/config/config.go:37-47` (Settings struct)
- Modify: `gohome/internal/config/config.go:70-121` (Load merge logic)
- Modify: `gohome/internal/config/config.go:172-266` (LoadAnnotated sources)

**Step 1: Write the failing test**

Add to `gohome/internal/config/config_test.go`:

```go
func TestLoad_MergesAutoCompactFields(t *testing.T) {
	dir := t.TempDir()

	global := Settings{
		AutoCompact:     true,
		AutoCompactMode: "percentage",
		AutoCompactPct:  0.85,
	}
	project := Settings{
		AutoCompactMode:      "leftover",
		AutoCompactLeftover:  64000,
		AutoCompactPrompt:    "custom prompt",
	}

	gPath := writeJSON(t, dir, "global.json", global)
	pPath := writeJSON(t, dir, "project.json", project)

	merged, err := Load(gPath, pPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Global autoCompact preserved (project didn't set it)
	if !merged.AutoCompact {
		t.Error("AutoCompact: got false, want true")
	}
	// Project overrides mode
	if merged.AutoCompactMode != "leftover" {
		t.Errorf("AutoCompactMode: got %q, want leftover", merged.AutoCompactMode)
	}
	// Global pct preserved (project didn't set it)
	if merged.AutoCompactPct != 0.85 {
		t.Errorf("AutoCompactPct: got %v, want 0.85", merged.AutoCompactPct)
	}
	// Project leftover wins
	if merged.AutoCompactLeftover != 64000 {
		t.Errorf("AutoCompactLeftover: got %d, want 64000", merged.AutoCompactLeftover)
	}
	// Project prompt wins
	if merged.AutoCompactPrompt != "custom prompt" {
		t.Errorf("AutoCompactPrompt: got %q, want 'custom prompt'", merged.AutoCompactPrompt)
	}
}

func TestLoad_AutoCompactZeroPreservesGlobal(t *testing.T) {
	dir := t.TempDir()

	global := Settings{
		AutoCompactPct:       0.90,
		AutoCompactTargetPct: 0.40,
		AutoCompactLeftover:  16000,
	}
	project := Settings{} // all zero — should not override

	gPath := writeJSON(t, dir, "global.json", global)
	pPath := writeJSON(t, dir, "project.json", project)

	merged, err := Load(gPath, pPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if merged.AutoCompactPct != 0.90 {
		t.Errorf("AutoCompactPct: got %v, want 0.90", merged.AutoCompactPct)
	}
	if merged.AutoCompactTargetPct != 0.40 {
		t.Errorf("AutoCompactTargetPct: got %v, want 0.40", merged.AutoCompactTargetPct)
	}
	if merged.AutoCompactLeftover != 16000 {
		t.Errorf("AutoCompactLeftover: got %d, want 16000", merged.AutoCompactLeftover)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/config/ -run "TestLoad_MergesAutoCompactFields|TestLoad_AutoCompactZeroPreservesGlobal" -v`
Expected: FAIL — fields do not exist on `Settings` struct yet.

**Step 3: Write minimal implementation**

In `gohome/internal/config/defaults.go`, add constants:

```go
const (
	DefaultAutoCompactPct       = 0.80
	DefaultAutoCompactTargetPct = 0.50
	DefaultAutoCompactLeftover  = 32_000
)

var DefaultAutoCompactPrompt = `You are summarizing a coding assistant conversation for context compaction.
Produce a concise summary that preserves:
- The user's current goal and any sub-tasks
- Key decisions made and their reasoning
- File paths and code changes discussed or made
- Any pending work or unresolved issues
- Tool results that are still relevant

Be factual and specific. Do not add commentary or analysis.
Write the summary as a narrative, not a bulleted list.`
```

In `gohome/internal/config/config.go`, add fields to `Settings`:

```go
type Settings struct {
	// ... existing fields ...
	AutoCompact          bool    `json:"autoCompact,omitempty"`
	AutoCompactMode      string  `json:"autoCompactMode,omitempty"`
	AutoCompactPct       float64 `json:"autoCompactPct,omitempty"`
	AutoCompactTargetPct float64 `json:"autoCompactTargetPct,omitempty"`
	AutoCompactLeftover  int     `json:"autoCompactLeftover,omitempty"`
	AutoCompactPrompt    string  `json:"autoCompactPrompt,omitempty"`
}
```

In the `Load` function, add merge logic for each new field following the existing pattern (project overrides global when non-zero):

```go
// In Load(), after existing merge lines:
if global.AutoCompact {
	merged.AutoCompact = true
}
if project.AutoCompact {
	merged.AutoCompact = true
}
if global.AutoCompactMode != "" {
	merged.AutoCompactMode = global.AutoCompactMode
}
if project.AutoCompactMode != "" {
	merged.AutoCompactMode = project.AutoCompactMode
}
if global.AutoCompactPct != 0 {
	merged.AutoCompactPct = global.AutoCompactPct
}
if project.AutoCompactPct != 0 {
	merged.AutoCompactPct = project.AutoCompactPct
}
if global.AutoCompactTargetPct != 0 {
	merged.AutoCompactTargetPct = global.AutoCompactTargetPct
}
if project.AutoCompactTargetPct != 0 {
	merged.AutoCompactTargetPct = project.AutoCompactTargetPct
}
if global.AutoCompactLeftover != 0 {
	merged.AutoCompactLeftover = global.AutoCompactLeftover
}
if project.AutoCompactLeftover != 0 {
	merged.AutoCompactLeftover = project.AutoCompactLeftover
}
if global.AutoCompactPrompt != "" {
	merged.AutoCompactPrompt = global.AutoCompactPrompt
}
if project.AutoCompactPrompt != "" {
	merged.AutoCompactPrompt = project.AutoCompactPrompt
}
```

In `LoadAnnotated`, add source tracking entries for each new field, following the existing pattern:

```go
// In the sources map initialiser:
"autoCompact":          SourceDefault,
"autoCompactMode":      SourceDefault,
"autoCompactPct":       SourceDefault,
"autoCompactTargetPct": SourceDefault,
"autoCompactLeftover":  SourceDefault,
"autoCompactPrompt":    SourceDefault,

// Then the global/project override checks:
if global.AutoCompact { sources["autoCompact"] = SourceGlobal }
if project.AutoCompact { sources["autoCompact"] = SourceProject }
if global.AutoCompactMode != "" { sources["autoCompactMode"] = SourceGlobal }
if project.AutoCompactMode != "" { sources["autoCompactMode"] = SourceProject }
if global.AutoCompactPct != 0 { sources["autoCompactPct"] = SourceGlobal }
if project.AutoCompactPct != 0 { sources["autoCompactPct"] = SourceProject }
if global.AutoCompactTargetPct != 0 { sources["autoCompactTargetPct"] = SourceGlobal }
if project.AutoCompactTargetPct != 0 { sources["autoCompactTargetPct"] = SourceProject }
if global.AutoCompactLeftover != 0 { sources["autoCompactLeftover"] = SourceGlobal }
if project.AutoCompactLeftover != 0 { sources["autoCompactLeftover"] = SourceProject }
if global.AutoCompactPrompt != "" { sources["autoCompactPrompt"] = SourceGlobal }
if project.AutoCompactPrompt != "" { sources["autoCompactPrompt"] = SourceProject }
```

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/config/ -run "TestLoad_MergesAutoCompactFields|TestLoad_AutoCompactZeroPreservesGlobal" -v`
Expected: PASS

**Step 5: Run full config test suite**

Run: `go test ./gohome/internal/config/ -v`
Expected: All tests PASS (no regressions).

**Step 6: Commit**

```bash
git add gohome/internal/config/defaults.go gohome/internal/config/config.go gohome/internal/config/config_test.go
git commit -m "feat(config): add auto-compact settings fields and merge logic"
```

---

### Task 2: Add EventCompacted and compact fields to agent events

**Files:**
- Modify: `gohome/internal/agent/events.go:14-27` (EventKind constants)
- Modify: `gohome/internal/agent/events.go:44-59` (Event struct)
- Modify: `gohome/internal/agent/events_test.go:40-62` (EventKind test)

**Step 1: Write the failing test**

Add to `gohome/internal/agent/events_test.go`, inside `TestEventKindConstants`:

```go
// Add to the cases slice:
{EventCompacted, "compacted"},
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/agent/ -run TestEventKindConstants -v`
Expected: FAIL — `EventCompacted` is undefined.

**Step 3: Write minimal implementation**

In `gohome/internal/agent/events.go`:

Add the new constant to the `EventKind` block:

```go
EventCompacted  EventKind = "compacted"
```

Add fields to the `Event` struct:

```go
CompactBefore int `json:"compactBefore,omitempty"`
CompactAfter  int `json:"compactAfter,omitempty"`
```

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/agent/ -run TestEventKindConstants -v`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/agent/events.go gohome/internal/agent/events_test.go
git commit -m "feat(agent): add EventCompacted kind and compact fields to Event"
```

---

### Task 3: Add Compaction event type to session persistence

**Files:**
- Modify: `gohome/internal/session/events.go:56-96` (add struct + encode case)
- Test: `gohome/internal/session/events_test.go`

**Step 1: Write the failing test**

Add to `gohome/internal/session/events_test.go`. First check what's in that file:

The test should verify that the `Compaction` event type encodes correctly via the `encode` function. Look at existing encode tests in that file and follow the same pattern. If no encode tests exist, write:

```go
func TestEncode_Compaction(t *testing.T) {
	ev := Compaction{
		BeforeTokens: 95000,
		AfterTokens:  40000,
		Summary:      "The user was working on...",
	}
	data, err := encode(ev)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["type"] != "compaction" {
		t.Errorf("type: got %v, want compaction", m["type"])
	}
	if m["ts"] == nil || m["ts"] == "" {
		t.Error("ts is missing or empty")
	}
	if int(m["beforeTokens"].(float64)) != 95000 {
		t.Errorf("beforeTokens: got %v, want 95000", m["beforeTokens"])
	}
	if int(m["afterTokens"].(float64)) != 40000 {
		t.Errorf("afterTokens: got %v, want 40000", m["afterTokens"])
	}
	if m["summary"] != "The user was working on..." {
		t.Errorf("summary: got %v", m["summary"])
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/session/ -run TestEncode_Compaction -v`
Expected: FAIL — `Compaction` type is undefined.

**Step 3: Write minimal implementation**

In `gohome/internal/session/events.go`, add the struct after `SessionEnd`:

```go
type Compaction struct {
	BeforeTokens int    `json:"beforeTokens"`
	AfterTokens  int    `json:"afterTokens"`
	Summary      string `json:"summary"`
}
```

Add a case to the `encode` function's type switch:

```go
case Compaction:
	typeName = "compaction"
```

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/session/ -run TestEncode_Compaction -v`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/session/events.go gohome/internal/session/events_test.go
git commit -m "feat(session): add Compaction event type for JSONL persistence"
```

---

### Task 4: Handle compaction events in session.Load

**Files:**
- Modify: `gohome/internal/session/load.go:50-122` (switch block)
- Modify: `gohome/internal/session/load_test.go`

**Step 1: Write the failing tests**

Add to `gohome/internal/session/load_test.go`:

```go
func TestLoad_WithCompaction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	w, err := OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}

	w.Emit(SessionStart{
		ID:    "sess-compact",
		CWD:   "/tmp",
		Model: "m",
	})
	// Pre-compaction messages
	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "old message 1"}}})
	w.Emit(AssistantMessage{Content: []common.Block{{Kind: common.BlockText, Text: "old reply 1"}}})
	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "old message 2"}}})
	w.Emit(AssistantMessage{Content: []common.Block{{Kind: common.BlockText, Text: "old reply 2"}}})

	// Compaction event
	w.Emit(Compaction{
		BeforeTokens: 50000,
		AfterTokens:  10000,
		Summary:      "Summary of conversation so far.",
	})

	// Post-compaction messages
	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "new message"}}})
	w.Emit(AssistantMessage{Content: []common.Block{{Kind: common.BlockText, Text: "new reply"}}})

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	sess, history, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if sess.ID != "sess-compact" {
		t.Errorf("sess.ID = %q", sess.ID)
	}

	// History should be: summary user message + new user + new assistant = 3
	if len(history) != 3 {
		t.Fatalf("len(history) = %d, want 3", len(history))
	}

	// First message is the compaction summary
	if history[0].Role != common.RoleUser {
		t.Errorf("history[0].Role = %q, want user", history[0].Role)
	}
	if history[0].Content[0].Text != "[Auto-compact summary]\n\nSummary of conversation so far." {
		t.Errorf("history[0] text = %q", history[0].Content[0].Text)
	}

	// Remaining messages are post-compaction
	if history[1].Content[0].Text != "new message" {
		t.Errorf("history[1] text = %q", history[1].Content[0].Text)
	}
	if history[2].Content[0].Text != "new reply" {
		t.Errorf("history[2] text = %q", history[2].Content[0].Text)
	}
}

func TestLoad_MultipleCompactions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	w, err := OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}

	w.Emit(SessionStart{ID: "sess-multi", CWD: "/tmp", Model: "m"})
	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "old"}}})

	// First compaction
	w.Emit(Compaction{BeforeTokens: 50000, AfterTokens: 10000, Summary: "first summary"})
	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "middle"}}})

	// Second compaction
	w.Emit(Compaction{BeforeTokens: 40000, AfterTokens: 8000, Summary: "second summary"})
	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "final"}}})

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, history, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Should have: second summary + "final" = 2 messages
	if len(history) != 2 {
		t.Fatalf("len(history) = %d, want 2", len(history))
	}
	if history[0].Content[0].Text != "[Auto-compact summary]\n\nsecond summary" {
		t.Errorf("history[0] text = %q", history[0].Content[0].Text)
	}
	if history[1].Content[0].Text != "final" {
		t.Errorf("history[1] text = %q", history[1].Content[0].Text)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/session/ -run "TestLoad_WithCompaction|TestLoad_MultipleCompactions" -v`
Expected: FAIL — `Compaction` event is not handled by `Load`.

**Step 3: Write minimal implementation**

In `gohome/internal/session/load.go`, add a case inside the `switch envelope.Type` block (after the `"tool_result"` case):

```go
case "compaction":
	var ev struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		continue
	}
	// Replace all accumulated history with the compaction summary.
	history = []common.Message{
		{
			Role: common.RoleUser,
			Content: []common.Block{
				{Kind: common.BlockText, Text: "[Auto-compact summary]\n\n" + ev.Summary},
			},
		},
	}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/session/ -run "TestLoad_WithCompaction|TestLoad_MultipleCompactions" -v`
Expected: PASS

**Step 5: Run full session test suite**

Run: `go test ./gohome/internal/session/ -v`
Expected: All tests PASS.

**Step 6: Commit**

```bash
git add gohome/internal/session/load.go gohome/internal/session/load_test.go
git commit -m "feat(session): handle compaction events in session.Load for resume"
```

---

### Task 5: Implement CompactConfig and compact logic

**Files:**
- Modify: `gohome/internal/agent/agent.go:12-28` (add CompactConfig and CompactPrompt fields)
- Create: `gohome/internal/agent/compact.go`
- Create: `gohome/internal/agent/compact_test.go`

**Step 1: Write failing tests for shouldCompact**

Create `gohome/internal/agent/compact_test.go`:

```go
package agent

import (
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

func TestShouldCompact_Disabled(t *testing.T) {
	cfg := CompactConfig{Enabled: false, Mode: "percentage", TriggerPct: 0.80, ContextWindow: 100000}
	usage := common.Usage{InputTokens: 90000, OutputTokens: 5000}
	if cfg.shouldCompact(usage) {
		t.Error("shouldCompact returned true when disabled")
	}
}

func TestShouldCompact_Percentage_Below(t *testing.T) {
	cfg := CompactConfig{Enabled: true, Mode: "percentage", TriggerPct: 0.80, ContextWindow: 100000}
	usage := common.Usage{InputTokens: 70000, OutputTokens: 5000} // 75% — below 80%
	if cfg.shouldCompact(usage) {
		t.Error("shouldCompact returned true below threshold")
	}
}

func TestShouldCompact_Percentage_Above(t *testing.T) {
	cfg := CompactConfig{Enabled: true, Mode: "percentage", TriggerPct: 0.80, ContextWindow: 100000}
	usage := common.Usage{InputTokens: 75000, OutputTokens: 10000} // 85% — above 80%
	if !cfg.shouldCompact(usage) {
		t.Error("shouldCompact returned false above threshold")
	}
}

func TestShouldCompact_Percentage_Exact(t *testing.T) {
	cfg := CompactConfig{Enabled: true, Mode: "percentage", TriggerPct: 0.80, ContextWindow: 100000}
	usage := common.Usage{InputTokens: 75000, OutputTokens: 5000} // exactly 80%
	if !cfg.shouldCompact(usage) {
		t.Error("shouldCompact returned false at exact threshold")
	}
}

func TestShouldCompact_Leftover_Above(t *testing.T) {
	cfg := CompactConfig{Enabled: true, Mode: "leftover", Leftover: 32000, ContextWindow: 128000}
	usage := common.Usage{InputTokens: 80000, OutputTokens: 10000} // 38k free — above 32k leftover
	if cfg.shouldCompact(usage) {
		t.Error("shouldCompact returned true when enough tokens remain")
	}
}

func TestShouldCompact_Leftover_Below(t *testing.T) {
	cfg := CompactConfig{Enabled: true, Mode: "leftover", Leftover: 32000, ContextWindow: 128000}
	usage := common.Usage{InputTokens: 90000, OutputTokens: 10000} // 28k free — below 32k leftover
	if !cfg.shouldCompact(usage) {
		t.Error("shouldCompact returned false when tokens below leftover")
	}
}

func TestShouldCompact_ZeroContextWindow(t *testing.T) {
	cfg := CompactConfig{Enabled: true, Mode: "percentage", TriggerPct: 0.80, ContextWindow: 0}
	usage := common.Usage{InputTokens: 90000, OutputTokens: 5000}
	if cfg.shouldCompact(usage) {
		t.Error("shouldCompact returned true with zero context window")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/agent/ -run "TestShouldCompact" -v`
Expected: FAIL — `CompactConfig` type is undefined.

**Step 3: Write the CompactConfig and shouldCompact implementation**

Create `gohome/internal/agent/compact.go`:

```go
package agent

import (
	"context"
	"log/slog"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

type CompactConfig struct {
	Enabled       bool
	Mode          string  // "percentage" or "leftover"
	TriggerPct    float64
	TargetPct     float64
	Leftover      int
	ContextWindow int
}

func (cfg CompactConfig) shouldCompact(usage common.Usage) bool {
	if !cfg.Enabled || cfg.ContextWindow <= 0 {
		return false
	}
	used := usage.InputTokens + usage.OutputTokens
	switch cfg.Mode {
	case "percentage":
		return float64(used)/float64(cfg.ContextWindow) >= cfg.TriggerPct
	case "leftover":
		return (cfg.ContextWindow - used) < cfg.Leftover
	}
	return false
}

func (a *Agent) compact(ctx context.Context, sess *session.Session) error {
	if len(sess.History) == 0 {
		return nil
	}

	prompt := a.CompactPrompt
	if prompt == "" {
		prompt = defaultCompactPrompt
	}

	beforeTokens := 0
	for _, msg := range sess.History {
		for _, b := range msg.Content {
			beforeTokens += len(b.Text) / 4 // rough estimate
		}
	}

	req := common.Request{
		Model:     sess.Model,
		System:    prompt,
		Messages:  sess.History,
		MaxTokens: a.MaxTokens,
	}

	events, err := a.State.Client().Stream(ctx, req)
	if err != nil {
		return err
	}

	var summary string
	for ev := range events {
		switch ev.Kind {
		case common.EventTextDelta:
			summary += ev.TextDelta
		case common.EventError:
			return ev.Err
		}
	}

	if summary == "" {
		slog.Warn("compact: LLM returned empty summary, skipping")
		return nil
	}

	// Replace history with the summary.
	sess.History = []common.Message{
		{
			Role: common.RoleUser,
			Content: []common.Block{
				{Kind: common.BlockText, Text: "[Auto-compact summary]\n\n" + summary},
			},
		},
	}

	afterTokens := len(summary) / 4 // rough estimate

	// Persist to JSONL.
	if w := a.State.Writer(); w != nil {
		w.Emit(session.Compaction{
			BeforeTokens: beforeTokens,
			AfterTokens:  afterTokens,
			Summary:      summary,
		})
	}

	// Notify frontend.
	a.Frontend.Emit(sess.ID, Event{
		Kind:          EventCompacted,
		SessionID:     sess.ID,
		CompactBefore: beforeTokens,
		CompactAfter:  afterTokens,
	})

	return nil
}

const defaultCompactPrompt = `You are summarizing a coding assistant conversation for context compaction.
Produce a concise summary that preserves:
- The user's current goal and any sub-tasks
- Key decisions made and their reasoning
- File paths and code changes discussed or made
- Any pending work or unresolved issues
- Tool results that are still relevant

Be factual and specific. Do not add commentary or analysis.
Write the summary as a narrative, not a bulleted list.`
```

Add fields to `Agent` struct in `gohome/internal/agent/agent.go`:

```go
CompactCfg    CompactConfig
CompactPrompt string
```

**Step 4: Run shouldCompact tests to verify they pass**

Run: `go test ./gohome/internal/agent/ -run "TestShouldCompact" -v`
Expected: All PASS

**Step 5: Write failing tests for compact()**

Add to `gohome/internal/agent/compact_test.go`:

```go
func TestCompact_ReplacesHistory(t *testing.T) {
	summaryText := "This is the compacted summary."
	events := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: summaryText},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	// Seed history with some messages.
	sess.History = []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "first"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "second"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply2"}}},
	}

	if err := a.compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}

	// History replaced with single summary message.
	if len(sess.History) != 1 {
		t.Fatalf("len(sess.History) = %d, want 1", len(sess.History))
	}
	if sess.History[0].Role != common.RoleUser {
		t.Errorf("role = %q, want user", sess.History[0].Role)
	}
	want := "[Auto-compact summary]\n\n" + summaryText
	if sess.History[0].Content[0].Text != want {
		t.Errorf("text = %q, want %q", sess.History[0].Content[0].Text, want)
	}
}

func TestCompact_EmitsEvent(t *testing.T) {
	events := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: "summary"},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	sess.History = []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "hello"}}},
	}

	if err := a.compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}

	var found bool
	for _, ev := range fe.events {
		if ev.Kind == EventCompacted {
			found = true
			if ev.CompactBefore <= 0 {
				t.Errorf("CompactBefore = %d, want > 0", ev.CompactBefore)
			}
		}
	}
	if !found {
		t.Error("no EventCompacted emitted")
	}
}

func TestCompact_NonFatalOnError(t *testing.T) {
	events := []common.StreamEvent{
		{Kind: common.EventError, Err: context.DeadlineExceeded},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	sess.History = []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "hello"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "world"}}},
	}
	origLen := len(sess.History)

	err := a.compact(context.Background(), sess)
	if err == nil {
		t.Fatal("expected error from compact, got nil")
	}

	// History should be unchanged.
	if len(sess.History) != origLen {
		t.Errorf("history length changed: got %d, want %d", len(sess.History), origLen)
	}
}

func TestCompact_EmptyHistoryNoop(t *testing.T) {
	fe := &fakeRecorder{}
	client := &fakeClient{}
	a, sess, _ := newTestAgent(t, client, fe)

	sess.History = nil
	if err := a.compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}

	// No events should have been emitted.
	if len(fe.events) != 0 {
		t.Errorf("expected no events, got %d", len(fe.events))
	}
	// Client should not have been called.
	if client.callCount != 0 {
		t.Errorf("client called %d times, want 0", client.callCount)
	}
}
```

**Step 6: Run compact tests to verify they pass**

Run: `go test ./gohome/internal/agent/ -run "TestCompact_" -v`
Expected: All PASS

**Step 7: Run full agent test suite**

Run: `go test ./gohome/internal/agent/ -v`
Expected: All tests PASS.

**Step 8: Commit**

```bash
git add gohome/internal/agent/compact.go gohome/internal/agent/compact_test.go gohome/internal/agent/agent.go
git commit -m "feat(agent): implement CompactConfig, shouldCompact, and compact logic"
```

---

### Task 6: Modify Turn to return usage, integrate compact into Run

**Files:**
- Modify: `gohome/internal/agent/turn.go:21` (change return type)
- Modify: `gohome/internal/agent/run.go:32` (use new return value, add compact check)
- Modify: `gohome/internal/agent/spawn.go` (child agent gets CompactCfg)
- Update: `gohome/internal/agent/turn_test.go` (all Turn callers)

**Step 1: Update Turn signature and all callers**

In `gohome/internal/agent/turn.go`, change the signature:

```go
// Before:
func (a *Agent) Turn(ctx context.Context, sess *session.Session) (string, error) {
// After:
func (a *Agent) Turn(ctx context.Context, sess *session.Session) (string, *common.Usage, error) {
```

Update all return statements in `turn.go`:
- `return "", nil, ctx.Err()` (line ~55, cancel case)
- `goto done` stays the same
- `return "", nil, ev.Err` (line ~114, error case)
- Final return at the end: `return stopReason, usage, nil` (line ~185)

In `gohome/internal/agent/run.go`, update the `Turn` call:

```go
// Before:
_, err := a.Turn(tctx, sess)
// After:
_, usage, err := a.Turn(tctx, sess)
```

After the error handling block (around line 45), add the compaction check:

```go
if usage != nil && a.CompactCfg.shouldCompact(*usage) {
	if compactErr := a.compact(tctx, sess); compactErr != nil {
		slog.Warn("auto-compact failed, continuing with full history", "err", compactErr)
	}
}
```

In `gohome/internal/agent/spawn.go`, line 88, copy `CompactCfg` to child:

```go
CompactCfg:      a.CompactCfg,
CompactPrompt:   a.CompactPrompt,
```

**Step 2: Update all Turn callers in tests**

In `gohome/internal/agent/turn_test.go`, update every `a.Turn(...)` call from:
```go
stopReason, err := a.Turn(...)
```
to:
```go
stopReason, _, err := a.Turn(...)
```

There are approximately 8 calls to update. Each `Turn` test that checks `stopReason` just needs the extra `_` for the usage return.

**Step 3: Run all agent tests**

Run: `go test ./gohome/internal/agent/ -v`
Expected: All tests PASS (including the new compact tests from Task 5).

**Step 4: Run full test suite to verify no regressions**

Run: `go test ./gohome/... -v`
Expected: All PASS.

**Step 5: Commit**

```bash
git add gohome/internal/agent/turn.go gohome/internal/agent/run.go gohome/internal/agent/spawn.go gohome/internal/agent/turn_test.go
git commit -m "feat(agent): return usage from Turn, integrate auto-compact into Run loop"
```

---

### Task 7: Handle EventCompacted in the TUI

**Files:**
- Modify: `gohome/internal/tui/model_agent.go:115-143` (add EventCompacted case)

**Step 1: Write the implementation**

In `gohome/internal/tui/model_agent.go`, add a new case in the `switch ev.Kind` block inside `handleAgentEvent`, after the `EventUsageUpdated` case (around line 126):

```go
case agent.EventCompacted:
	beforeK := ev.CompactBefore / 1000
	afterK := ev.CompactAfter / 1000
	sv.Timeline = append(sv.Timeline, TimelineEntry{
		Kind: KindNotice,
		Text: fmt.Sprintf("Context compacted: %dk -> %dk tokens", beforeK, afterK),
	})
	sv.warned80 = false
	sv.warned95 = false
```

Make sure `fmt` is imported (it likely already is; check the import block).

**Step 2: Run TUI tests**

Run: `go test ./gohome/internal/tui/ -v`
Expected: All tests PASS. Snapshot tests should not need updating since this is a new code path that existing snapshots do not exercise.

**Step 3: Commit**

```bash
git add gohome/internal/tui/model_agent.go
git commit -m "feat(tui): handle EventCompacted with timeline notice and warning reset"
```

---

### Task 8: Wire CompactConfig from settings into Agent in main.go

**Files:**
- Modify: `gohome/cmd/gohome/main.go:391-403` (agent construction)

**Step 1: Write the implementation**

In `gohome/cmd/gohome/main.go`, after the `thinkingBudget` resolution (around line 389) and before the `state :=` line (line 391), add:

```go
compactCfg := agent.CompactConfig{
	Enabled:       settings.AutoCompact,
	Mode:          settings.AutoCompactMode,
	TriggerPct:    settings.AutoCompactPct,
	TargetPct:     settings.AutoCompactTargetPct,
	Leftover:      settings.AutoCompactLeftover,
	ContextWindow: contextWindow,
}
if compactCfg.Mode == "" {
	compactCfg.Mode = "percentage"
}
if compactCfg.TriggerPct <= 0 {
	compactCfg.TriggerPct = config.DefaultAutoCompactPct
}
if compactCfg.TargetPct <= 0 {
	compactCfg.TargetPct = config.DefaultAutoCompactTargetPct
}
if compactCfg.Leftover <= 0 {
	compactCfg.Leftover = config.DefaultAutoCompactLeftover
}
```

Then in the agent construction (line 393-403), add the new fields:

```go
a := &agent.Agent{
	// ... existing fields ...
	CompactCfg:    compactCfg,
	CompactPrompt: settings.AutoCompactPrompt,
}
```

**Step 2: Build to verify compilation**

Run: `go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome`
Expected: Build succeeds with no errors.

**Step 3: Run full test suite**

Run: `go test ./gohome/...`
Expected: All tests PASS.

**Step 4: Commit**

```bash
git add gohome/cmd/gohome/main.go
git commit -m "feat(main): wire CompactConfig from settings into agent"
```

---

### Task 9: Lint and vet

**Step 1: Run vet**

Run: `go vet ./gohome/...`
Expected: No issues.

**Step 2: Run linter**

Run: `golangci-lint run ./gohome/...`
Expected: No issues. Fix any findings before proceeding.

**Step 3: Run full test suite one more time**

Run: `go test ./gohome/...`
Expected: All PASS.

**Step 4: Commit any lint fixes if needed**

```bash
git add -A && git commit -m "chore: fix lint findings"
```

---

### Task 10: Final build and smoke test

**Step 1: Build**

Run: `go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome`
Expected: Binary builds successfully.

**Step 2: Verify binary starts**

Run: `./bin/gohome --version`
Expected: Prints `gohome dev`.

**Step 3: Verify config flag shows new fields**

Run: `./bin/gohome --config 2>/dev/null | grep -i compact` (only if a valid config exists)
Expected: Shows `autoCompact`, `autoCompactMode`, etc. fields (all at zero/false defaults).

**Step 4: Commit if any final changes needed**

No commit expected unless fixes were needed.
