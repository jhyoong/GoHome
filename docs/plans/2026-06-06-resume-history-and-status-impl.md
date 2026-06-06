# Resume History Loading & Status Text Fix — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** When resuming a session, display all prior messages in the timeline and clear the "Resumed: id" status text after the user sends a message.

**Architecture:** Change `ResumeSession` callback to return loaded history alongside the error. Add a `historyToTimeline` conversion function that maps `[]common.Message` to `[]TimelineEntry`. Clear `statusMsg` in the message-send path.

**Tech Stack:** Go, Bubble Tea (charmbracelet/bubbletea), teatest

---

### Task 1: Change `ResumeSession` callback signature

**Files:**
- Modify: `gohome/internal/tui/slash.go:9`
- Modify: `gohome/cmd/gohome/main.go:282-312`
- Modify: `gohome/internal/tui/model.go:935-939`

**Step 1: Update the SlashCallbacks struct**

In `gohome/internal/tui/slash.go`, change the `ResumeSession` field. Add the `common` import.

```go
package tui

import (
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

type SlashCallbacks struct {
	NewSession    func() (string, error)
	ResumeSession func(id string) ([]common.Message, error)
	CancelSession func(id string)
	SetModel      func(name string) error
	ListSessions  func() ([]session.Listing, error)
}
```

**Step 2: Update main.go to return history**

In `gohome/cmd/gohome/main.go`, change the `ResumeSession` closure to capture and return the history:

Line 282: change signature from `func(id string) error` to `func(id string) ([]common.Message, error)`
Line 297: change `loaded, _, err := session.Load(path)` to `loaded, history, err := session.Load(path)`
Error returns: change `return err` to `return nil, err` and `return fmt.Errorf(...)` to `return nil, fmt.Errorf(...)`
Success return: change `return nil` to `return history, nil`

```go
ResumeSession: func(id string) ([]common.Message, error) {
	listings, err := session.List(home, cwd)
	if err != nil {
		return nil, err
	}
	var path string
	for _, l := range listings {
		if l.ID == id {
			path = l.Path
			break
		}
	}
	if path == "" {
		return nil, fmt.Errorf("session %q not found", id)
	}
	loaded, history, err := session.Load(path)
	if err != nil {
		return nil, err
	}
	writer.Emit(session.SessionEnd{Reason: "switch"})
	_ = writer.Close()

	newWriter, err := session.OpenWriter(path)
	if err != nil {
		return nil, fmt.Errorf("open writer: %w", err)
	}
	sess = loaded
	writer = newWriter
	a.Session = sess
	a.Writer = writer
	return history, nil
},
```

**Step 3: Update the TUI callback call site in model.go**

In `gohome/internal/tui/model.go`, inside the `SetOnSelect` callback (around line 935), change:

```go
if err := m.slashCB.ResumeSession(id); err != nil {
```

to:

```go
history, err := m.slashCB.ResumeSession(id)
if err != nil {
```

The `history` variable will be used in Task 2.

**Step 4: Verify compilation**

Run: `cd /Users/macminijh/projects/GoHome && go build ./gohome/...`
Expected: compiles with no errors

**Step 5: Run existing tests**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/... -run TestSlashResume -v`
Expected: existing tests still pass (they don't set `ResumeSession` callback, so the nil check at line 935 skips it)

**Step 6: Commit**

```bash
git add gohome/internal/tui/slash.go gohome/cmd/gohome/main.go gohome/internal/tui/model.go
git commit -m "refactor: change ResumeSession callback to return history"
```

---

### Task 2: Add `historyToTimeline` conversion function

**Files:**
- Create: `gohome/internal/tui/history_convert.go`
- Create: `gohome/internal/tui/history_convert_test.go`

**Step 1: Write the failing tests**

Create `gohome/internal/tui/history_convert_test.go`:

```go
package tui

import (
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

func TestHistoryToTimeline_UserMessage(t *testing.T) {
	msgs := []common.Message{
		{Role: common.RoleUser, Content: []common.Block{
			{Kind: common.BlockText, Text: "hello world"},
		}},
	}
	got := historyToTimeline(msgs)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Kind != "user" || got[0].Text != "hello world" {
		t.Errorf("got %+v", got[0])
	}
}

func TestHistoryToTimeline_AssistantTextAndThinking(t *testing.T) {
	msgs := []common.Message{
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockThinking, Text: "let me think"},
			{Kind: common.BlockText, Text: "here is the answer"},
		}},
	}
	got := historyToTimeline(msgs)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Kind != "thinking" || got[0].Text != "let me think" {
		t.Errorf("thinking entry: %+v", got[0])
	}
	if got[1].Kind != "assistant" || got[1].Text != "here is the answer" {
		t.Errorf("assistant entry: %+v", got[1])
	}
}

func TestHistoryToTimeline_ToolUseAndResult(t *testing.T) {
	msgs := []common.Message{
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockToolUse, ToolName: "bash", ToolUseID: "t1", InputJSON: `{"cmd":"ls"}`},
		}},
		{Role: common.RoleTool, Content: []common.Block{
			{Kind: common.BlockToolResult, ToolUseID: "t1", ResultText: "file.go", IsError: false},
		}},
	}
	got := historyToTimeline(msgs)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (tool use + result merged)", len(got))
	}
	if got[0].Kind != "tool" || got[0].ToolName != "bash" {
		t.Errorf("tool entry: %+v", got[0])
	}
	if got[0].Text != `{"cmd":"ls"}` {
		t.Errorf("tool input: %q", got[0].Text)
	}
	if got[0].ToolResult != "file.go" {
		t.Errorf("tool result: %q", got[0].ToolResult)
	}
	if got[0].Status != "success" {
		t.Errorf("tool status: %q", got[0].Status)
	}
}

func TestHistoryToTimeline_ToolError(t *testing.T) {
	msgs := []common.Message{
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockToolUse, ToolName: "bash", ToolUseID: "t2", InputJSON: `{"cmd":"fail"}`},
		}},
		{Role: common.RoleTool, Content: []common.Block{
			{Kind: common.BlockToolResult, ToolUseID: "t2", ResultText: "command not found", IsError: true},
		}},
	}
	got := historyToTimeline(msgs)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Status != "error" {
		t.Errorf("status = %q, want error", got[0].Status)
	}
}

func TestHistoryToTimeline_MultipleUserBlocks(t *testing.T) {
	msgs := []common.Message{
		{Role: common.RoleUser, Content: []common.Block{
			{Kind: common.BlockText, Text: "line one"},
			{Kind: common.BlockText, Text: "line two"},
		}},
	}
	got := historyToTimeline(msgs)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Text != "line one\nline two" {
		t.Errorf("text = %q", got[0].Text)
	}
}

func TestHistoryToTimeline_Empty(t *testing.T) {
	got := historyToTimeline(nil)
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}

func TestHistoryToTimeline_FullConversation(t *testing.T) {
	msgs := []common.Message{
		{Role: common.RoleUser, Content: []common.Block{
			{Kind: common.BlockText, Text: "fix the bug"},
		}},
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockThinking, Text: "analyzing"},
			{Kind: common.BlockText, Text: "I see the issue"},
			{Kind: common.BlockToolUse, ToolName: "edit", ToolUseID: "t1", InputJSON: `{"file":"main.go"}`},
		}},
		{Role: common.RoleTool, Content: []common.Block{
			{Kind: common.BlockToolResult, ToolUseID: "t1", ResultText: "ok"},
		}},
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockText, Text: "Fixed it"},
		}},
	}
	got := historyToTimeline(msgs)
	// Expected: user, thinking, assistant, tool(merged), assistant
	if len(got) != 5 {
		t.Fatalf("len = %d, want 5", len(got))
	}
	kinds := make([]string, len(got))
	for i, e := range got {
		kinds[i] = e.Kind
	}
	want := []string{"user", "thinking", "assistant", "tool", "assistant"}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("entry[%d].Kind = %q, want %q", i, kinds[i], want[i])
		}
	}
	if got[3].ToolResult != "ok" {
		t.Errorf("tool result not merged: %q", got[3].ToolResult)
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestHistoryToTimeline -v`
Expected: FAIL — `historyToTimeline` undefined

**Step 3: Write the implementation**

Create `gohome/internal/tui/history_convert.go`:

```go
package tui

import (
	"strings"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// historyToTimeline converts loaded session history into timeline entries
// for display in the chat area.
func historyToTimeline(msgs []common.Message) []TimelineEntry {
	var entries []TimelineEntry
	for _, msg := range msgs {
		switch msg.Role {
		case common.RoleUser:
			var parts []string
			for _, b := range msg.Content {
				if b.Kind == common.BlockText && b.Text != "" {
					parts = append(parts, b.Text)
				}
			}
			if len(parts) > 0 {
				entries = append(entries, TimelineEntry{
					Kind: "user",
					Text: strings.Join(parts, "\n"),
				})
			}

		case common.RoleAssistant:
			for _, b := range msg.Content {
				switch b.Kind {
				case common.BlockThinking:
					entries = append(entries, TimelineEntry{
						Kind: "thinking",
						Text: b.Text,
					})
				case common.BlockText:
					entries = append(entries, TimelineEntry{
						Kind: "assistant",
						Text: b.Text,
					})
				case common.BlockToolUse:
					entries = append(entries, TimelineEntry{
						Kind:     "tool",
						ToolName: b.ToolName,
						Text:     b.InputJSON,
						Status:   "success",
					})
				}
			}

		case common.RoleTool:
			for _, b := range msg.Content {
				if b.Kind != common.BlockToolResult {
					continue
				}
				status := "success"
				if b.IsError {
					status = "error"
				}
				merged := false
				for i := len(entries) - 1; i >= 0; i-- {
					if entries[i].Kind == "tool" && entries[i].ToolResult == "" {
						entries[i].ToolResult = b.ResultText
						entries[i].Status = status
						merged = true
						break
					}
				}
				if !merged {
					entries = append(entries, TimelineEntry{
						Kind:       "tool",
						ToolResult: b.ResultText,
						Status:     status,
					})
				}
			}
		}
	}
	return entries
}
```

**Step 4: Run the tests to verify they pass**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestHistoryToTimeline -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add gohome/internal/tui/history_convert.go gohome/internal/tui/history_convert_test.go
git commit -m "feat(tui): add historyToTimeline conversion for session resume"
```

---

### Task 3: Wire history into the resume handler

**Files:**
- Modify: `gohome/internal/tui/model.go:932-945`

**Step 1: Write the failing integration test**

Add to `gohome/internal/tui/integration_test.go`:

```go
func TestSlashResumeLoadsHistory(t *testing.T) {
	m := tui.New(nil, "")
	m.SetSlashCallbacks(tui.SlashCallbacks{
		ListSessions: func() ([]session.Listing, error) {
			return []session.Listing{
				{ID: "s1", Title: "test session"},
			}, nil
		},
		ResumeSession: func(id string) ([]common.Message, error) {
			return []common.Message{
				{Role: common.RoleUser, Content: []common.Block{
					{Kind: common.BlockText, Text: "previous question"},
				}},
				{Role: common.RoleAssistant, Content: []common.Block{
					{Kind: common.BlockText, Text: "previous answer"},
				}},
			}, nil
		},
	})

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Type("/resume")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("test session"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Select the session (Enter on the first item).
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Historical messages should appear in the rendered output.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("previous question")) &&
			bytes.Contains(out, []byte("previous answer"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
```

Add the `common` import to the test file's imports:

```go
import (
	...
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	...
)
```

**Step 2: Run the test to verify it fails**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/... -run TestSlashResumeLoadsHistory -v`
Expected: FAIL — the output won't contain "previous question" because history isn't wired in yet

**Step 3: Wire history into the SetOnSelect callback**

In `gohome/internal/tui/model.go`, update the `SetOnSelect` callback inside the `/resume` handler (around lines 932-945). After the `ResumeSession` call succeeds, convert history to timeline entries and populate the session view:

Change this block:

```go
sb.SetOnSelect(func(id string) {
	m.browsing = false
	m.sessionBrowser = nil
	if m.slashCB.ResumeSession != nil {
		if err := m.slashCB.ResumeSession(id); err != nil {
			m.statusMsg = fmt.Sprintf("/resume: %v", err)
			return
		}
	}
	m.getOrCreateSession(id, 0)
	m.focused = id
	m.cursor = 0
	m.statusMsg = "Resumed: " + id
	m.rebuildViewport()
})
```

To:

```go
sb.SetOnSelect(func(id string) {
	m.browsing = false
	m.sessionBrowser = nil
	var history []common.Message
	if m.slashCB.ResumeSession != nil {
		var err error
		history, err = m.slashCB.ResumeSession(id)
		if err != nil {
			m.statusMsg = fmt.Sprintf("/resume: %v", err)
			return
		}
	}
	sv := m.getOrCreateSession(id, 0)
	sv.Timeline = historyToTimeline(history)
	m.focused = id
	m.cursor = len(sv.Timeline) - 1
	m.statusMsg = "Resumed: " + id
	m.rebuildViewport()
})
```

Add the `common` import to model.go's imports:

```go
"github.com/jhyoong/GoHome/gohome/internal/llm/common"
```

**Step 4: Run the test to verify it passes**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/... -run TestSlashResumeLoadsHistory -v`
Expected: PASS

**Step 5: Run all TUI tests to check for regressions**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/... -v`
Expected: all PASS

**Step 6: Commit**

```bash
git add gohome/internal/tui/model.go gohome/internal/tui/integration_test.go
git commit -m "feat(tui): load session history into timeline on resume"
```

---

### Task 4: Clear statusMsg on message send

**Files:**
- Modify: `gohome/internal/tui/model.go:650-659`
- Modify: `gohome/internal/tui/integration_test.go` (new test)

**Step 1: Write the failing test**

Add to `gohome/internal/tui/integration_test.go`:

```go
func TestStatusMsgClearedOnSend(t *testing.T) {
	fe := tui.NewFrontend()
	m := tui.New(fe, "")
	m.SetSlashCallbacks(tui.SlashCallbacks{
		ListSessions: func() ([]session.Listing, error) {
			return []session.Listing{
				{ID: "s1", Title: "test session"},
			}, nil
		},
		ResumeSession: func(id string) ([]common.Message, error) {
			return nil, nil
		},
	})

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	// Drain input channel so sends don't block.
	go func() {
		for range fe.InputCh() {
		}
	}()

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Open resume browser.
	tm.Type("/resume")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("test session"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Select the session.
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// "Resumed:" should appear.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Resumed:"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Type and send a message.
	tm.Type("hello")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// "Resumed:" should no longer appear — verify by waiting for the user
	// message to render and checking that "Resumed:" is absent.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("hello")) && !bytes.Contains(out, []byte("Resumed:"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
```

**Step 2: Run the test to verify it fails**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/... -run TestStatusMsgClearedOnSend -v`
Expected: FAIL — "Resumed:" still present after sending a message

**Step 3: Add the fix**

In `gohome/internal/tui/model.go`, in the normal message-send path (around line 656), add `m.statusMsg = ""` after `m.editor.SetValue("")`:

Change:

```go
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
```

To:

```go
} else {
	sv.Timeline = append(sv.Timeline, TimelineEntry{
		Kind: "user",
		Text: text,
	})
	sv.InFlight = true
	m.editor.SetValue("")
	m.statusMsg = ""
	m.cursor = len(sv.Timeline) - 1
	m.rebuildViewport()
	cmds = append(cmds, m.sendInputCmd(text))
}
```

**Step 4: Run the test to verify it passes**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/... -run TestStatusMsgClearedOnSend -v`
Expected: PASS

**Step 5: Run all tests for regressions**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/... -v`
Expected: all PASS

**Step 6: Commit**

```bash
git add gohome/internal/tui/model.go gohome/internal/tui/integration_test.go
git commit -m "fix(tui): clear statusMsg when user sends a message"
```

---

### Task 5: Final verification

**Step 1: Run the full test suite**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/... -v`
Expected: all PASS

**Step 2: Build the binary**

Run: `cd /Users/macminijh/projects/GoHome && go build ./gohome/...`
Expected: compiles with no errors

**Step 3: Verify git log**

Run: `git log --oneline -5`
Expected: 4 new commits (signature change, conversion function, wiring, statusMsg fix)
