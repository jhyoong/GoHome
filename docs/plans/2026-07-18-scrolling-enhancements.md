# Scrolling Enhancements Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix two scrolling issues: (1) let the user scroll the timeline while an approval prompt is active, and (2) preserve manual scroll position when new agent content arrives.

**Architecture:** Add Bubble Tea mouse support so mouse wheel events scroll the timeline independently of arrow keys. Add PgUp/PgDown passthrough during approval. Change `rebuildViewport()` to preserve scroll state instead of forcing scroll-to-bottom.

**Tech Stack:** Go, Bubble Tea v1.3.10 (`tea.MouseMsg`, `tea.WithMouseCellMotion`)

---

### Task 1: Smart auto-scroll -- change rebuildViewport to preserve scroll state

**Files:**
- Modify: `gohome/internal/tui/model.go:330-347` (`rebuildViewport`)

**Step 1: Write the failing test**

Add to `gohome/internal/tui/chat_test.go`:

```go
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
```

This test requires a `ScrollTop()` accessor that doesn't exist yet. Add it in step 3.

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/tui/ -run TestSmartAutoScroll_PreservesManualScroll -v`
Expected: Compile error -- `ScrollTop()` method not found.

**Step 3: Implement the changes**

3a. Add `ScrollTop()` accessor to `gohome/internal/tui/chat.go` after `IsAutoScroll()` (line 108):

```go
// ScrollTop returns the current scroll offset (exported for tests).
func (c *ChatComponent) ScrollTop() int { return c.scrollTop }
```

3b. Change `rebuildViewport` in `gohome/internal/tui/model.go:330-347`. Remove the `keepScroll ...bool` variadic parameter. Remove the conditional `ScrollToBottom()` call. The method now always preserves the current scroll mode:

```go
// rebuildViewport refreshes the chat component state from the focused session.
func (m *Model) rebuildViewport() {
	sv, ok := m.sessions[m.focused]
	if !ok {
		return
	}
	cur := -1
	if strings.TrimSpace(m.editor.Value()) == "" {
		m.clampCursor()
		cur = m.cursor
	}
	m.chat.SetTimeline(&sv.Timeline)
	m.chat.SetCursor(cur)
}
```

3c. Update all callers that passed `keepScroll` arguments. These callers in `gohome/internal/tui/model_keys.go` previously passed `true` to preserve scroll -- now they just call `rebuildViewport()` since that's the default:

- Line 118: `m.rebuildViewport(true)` -> `m.rebuildViewport()`
- Line 194: `m.rebuildViewport(true)` -> `m.rebuildViewport()`
- Line 209: `m.rebuildViewport(true)` -> `m.rebuildViewport()`

3d. Add explicit `ScrollToBottom()` calls before `rebuildViewport()` at the call sites that need to force scroll-to-bottom (these are places where the user's context changes and they need to see the latest content):

In `gohome/internal/tui/model_keys.go`, user submit path (line 149):
```go
m.chat.ScrollToBottom()
m.rebuildViewport()
```

In `gohome/internal/tui/model_keys.go`, slash command path (line 124):
```go
m.chat.ScrollToBottom()
m.rebuildViewport()
```

In `gohome/internal/tui/model.go`, `cancelFocusedSessionWith` (line 365):
```go
m.chat.ScrollToBottom()
m.rebuildViewport()
```

In `gohome/internal/tui/model.go`, `AddTimelineEntry` (line 475):
```go
m.chat.ScrollToBottom()
m.rebuildViewport()
```

In `gohome/internal/tui/strip.go`, `focusNext` (line 52) and `focusPrev` (line 62):
```go
m.chat.ScrollToBottom()
m.rebuildViewport()
```

In `gohome/internal/tui/model_slash.go`, `/resume` callback (line 118):
```go
m.chat.ScrollToBottom()
m.rebuildViewport()
```

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/tui/ -run TestSmartAutoScroll -v`
Expected: PASS

Run: `go test ./gohome/internal/tui/ -v`
Expected: All existing tests still PASS (no regressions).

**Step 5: Commit**

```bash
git add gohome/internal/tui/chat.go gohome/internal/tui/model.go gohome/internal/tui/model_keys.go gohome/internal/tui/model_agent.go gohome/internal/tui/strip.go gohome/internal/tui/model_slash.go gohome/internal/tui/chat_test.go
git commit -m "feat: smart auto-scroll preserves manual scroll position

rebuildViewport() no longer forces scroll-to-bottom. Explicit
ScrollToBottom() is called only at context-switching call sites
(user submit, slash commands, session switching, cancel)."
```

---

### Task 2: PgUp/PgDown during approval prompts

**Files:**
- Modify: `gohome/internal/tui/model_approval.go:72-117` (top-level approval menu)

**Step 1: Write the failing test**

Add to `gohome/internal/tui/chat_test.go`:

```go
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
```

This test requires a `Chat()` accessor. Add it in step 3.

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/tui/ -run TestApprovalPgUpScrollsTimeline -v`
Expected: Compile error -- `Chat()` method not found. After adding accessor: FAIL because PgUp is currently ignored during approval.

**Step 3: Implement the changes**

3a. Add `Chat()` accessor to `gohome/internal/tui/model.go` after the `Yolo()` accessor (around line 613):

```go
// Chat returns the chat component (exported for tests).
func (m *Model) Chat() *ChatComponent { return m.chat }
```

3b. Add PgUp/PgDown handling in `gohome/internal/tui/model_approval.go`, in the top-level approval menu section. Insert before the existing `switch` at line 73:

```go
	// --- top-level approval menu ---
	// PgUp/PgDown scroll the timeline even during approval.
	if msg.Type == tea.KeyPgUp || msg.Type == tea.KeyPgDown {
		scrollAmt := m.chat.maxHeight / 2
		if scrollAmt < 1 {
			scrollAmt = 1
		}
		m.chat.DisableAutoScroll(m.winW)
		if msg.Type == tea.KeyPgUp {
			m.chat.ScrollUp(scrollAmt)
		} else {
			m.chat.ScrollDown(scrollAmt)
		}
		return tea.Batch(cmds...)
	}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/tui/ -run TestApprovalPgUpScrollsTimeline -v`
Expected: PASS

Run: `go test ./gohome/internal/tui/ -v`
Expected: All existing tests still PASS.

**Step 5: Commit**

```bash
git add gohome/internal/tui/model_approval.go gohome/internal/tui/model.go gohome/internal/tui/chat_test.go
git commit -m "feat: PgUp/PgDown scroll timeline during approval prompts

Adds page scroll handling in the approval key handler so users
can review diffs while deciding on tool approval."
```

---

### Task 3: Mouse wheel support

**Files:**
- Modify: `gohome/cmd/gohome/main.go:371` (program init)
- Modify: `gohome/internal/tui/model.go:368-437` (`Update` method)

**Step 1: Write the failing test**

Add to `gohome/internal/tui/chat_test.go`:

```go
func TestMouseWheelUpScrollsTimeline(t *testing.T) {
	m := New(nil, "main")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	sv := m.Sessions()["main"]
	for i := 0; i < 50; i++ {
		sv.Timeline = append(sv.Timeline, TimelineEntry{Kind: KindUser, Text: fmt.Sprintf("msg %d", i)})
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Mouse wheel up should scroll the timeline.
	m.Update(tea.MouseMsg{Type: tea.MouseWheelUp})

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

	// Activate approval.
	ch := make(chan guard.ApprovalDecision, 1)
	m.Update(ApprovalReqMsg{
		Req:   guard.ApprovalRequest{SessionID: "main", Tool: "shell", Input: json.RawMessage(`{"command":"ls"}`)},
		Reply: ch,
	})

	// Mouse wheel should still scroll timeline during approval.
	m.Update(tea.MouseMsg{Type: tea.MouseWheelUp})

	if m.Chat().IsAutoScroll() {
		t.Error("expected autoScroll to be false after mouse wheel up during approval")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/tui/ -run TestMouseWheel -v`
Expected: FAIL -- `tea.MouseMsg` is not handled in `Update()`, so autoScroll stays true.

**Step 3: Implement the changes**

3a. Add `tea.MouseMsg` handler in `gohome/internal/tui/model.go` `Update()` method. Insert after the `approvalReqMsg` case (line 383), before the `tea.KeyMsg` case:

```go
	case tea.MouseMsg:
		switch msg.Type {
		case tea.MouseWheelUp:
			m.chat.DisableAutoScroll(m.winW)
			m.chat.ScrollUp(3)
		case tea.MouseWheelDown:
			m.chat.DisableAutoScroll(m.winW)
			m.chat.ScrollDown(3)
		}
```

3b. Enable mouse support in `gohome/cmd/gohome/main.go` line 371:

```go
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
```

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/tui/ -run TestMouseWheel -v`
Expected: PASS

Run: `go test ./gohome/internal/tui/ -v`
Expected: All tests PASS.

Run: `go vet ./gohome/...`
Expected: No issues.

**Step 5: Commit**

```bash
git add gohome/cmd/gohome/main.go gohome/internal/tui/model.go gohome/internal/tui/chat_test.go
git commit -m "feat: add mouse wheel support for timeline scrolling

Mouse wheel scrolls the timeline regardless of active approval
prompts or modals. Uses tea.WithMouseCellMotion for event
detection. 3 lines per wheel tick."
```

---

### Task 4: Update snapshot golden files and run full test suite

**Files:**
- Possibly modify: `gohome/internal/tui/testdata/TestSnapshots/` (golden files)

**Step 1: Run existing snapshot tests**

Run: `go test ./gohome/internal/tui/ -run TestSnapshots -v`

If any fail due to scroll behavior changes, regenerate:

Run: `go test ./gohome/internal/tui/ -run TestSnapshots -update`

Then review the diff to confirm changes are only scroll-related.

**Step 2: Run the full test suite**

Run: `go test ./gohome/... -v`
Expected: All PASS.

**Step 3: Run linting**

Run: `go vet ./gohome/...`
Expected: No issues.

Run: `golangci-lint run ./gohome/...`
Expected: No issues.

**Step 4: Commit if golden files changed**

```bash
git add gohome/internal/tui/testdata/
git commit -m "test: update snapshot golden files for scrolling changes"
```

---

### Task 5: Manual verification

Build and run the application to manually verify all three changes work correctly.

**Step 1: Build**

```bash
go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome
```

**Step 2: Test mouse wheel scrolling**

- Start a conversation and generate enough output to overflow the viewport
- Use the mouse wheel to scroll up/down -- the timeline should scroll
- Trigger a tool call that requires approval
- While approval is visible, use the mouse wheel -- the timeline should scroll, not the approval menu
- Arrow up/down should still navigate approval options

**Step 3: Test PgUp/PgDown during approval**

- Trigger a tool call requiring approval
- Press PgUp/PgDown -- the timeline should scroll by half a page
- Arrow keys should still navigate approval options

**Step 4: Test smart auto-scroll**

- Scroll up manually during agent output generation
- Verify the viewport stays where you scrolled (does not jump to bottom)
- Scroll to the bottom manually -- auto-scroll should re-engage
- Submit a new message -- viewport should jump to bottom to show the response
