# Session Browser, Model Selector, Cancellable Spinner — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add three P2 TUI features: a `/sessions` browser to pick past sessions, a `/model` selector to switch endpoints, and Escape-to-cancel on the spinner.

**Architecture:** All three features share a generic `SelectListComponent` that implements `Component` + `Interactive`. Session browser and model selector are thin wrappers. The spinner gains a `HandleInput` method and an `onCancel` callback. Both selectors use the "editor-area replacement" pattern (same as file search popup).

**Tech Stack:** Go, Bubbletea, lipgloss. Tests use `testing` (unit) and `teatest` (integration). Existing helpers: `StripAnsi`, `VisualWidth`, `TruncateText` from `ansi.go`.

**Test command:** `go test ./gohome/internal/tui/... -count=1 -v`

**Module path:** `github.com/jhyoong/GoHome`

**Design doc:** `docs/plans/2026-06-06-session-model-loader-design.md`

---

### Task 1: SelectListComponent — Tests

**Files:**
- Create: `gohome/internal/tui/selectlist_test.go`

**Step 1: Write the tests**

Create `selectlist_test.go` in **package `tui`** (internal tests, same as `spinner_test.go`). This gives direct access to unexported fields.

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func threeItems() []SelectItem {
	return []SelectItem{
		{Value: "a", Label: "Alpha", Description: "first"},
		{Value: "b", Label: "Beta", Description: "second"},
		{Value: "c", Label: "Charlie", Description: "third"},
	}
}

func TestSelectListRenderShowsItems(t *testing.T) {
	sl := NewSelectList(threeItems(), nil)
	lines := sl.Render(80)
	// First line is the search input "> ".
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines (search + 3 items), got %d", len(lines))
	}
	// Selected item (index 0) should have "->" prefix.
	plain := StripAnsi(lines[1])
	if !strings.HasPrefix(plain, "->") {
		t.Errorf("selected item should start with '->': %q", plain)
	}
	if !strings.Contains(plain, "Alpha") {
		t.Errorf("first item should contain 'Alpha': %q", plain)
	}
	// Non-selected item should have "   " prefix.
	plain2 := StripAnsi(lines[2])
	if strings.HasPrefix(plain2, "->") {
		t.Errorf("second item should not start with '->': %q", plain2)
	}
}

func TestSelectListDescriptionShownWhenWide(t *testing.T) {
	sl := NewSelectList(threeItems(), nil)
	lines := sl.Render(80)
	plain := StripAnsi(lines[1])
	if !strings.Contains(plain, "first") {
		t.Errorf("description should appear at width=80: %q", plain)
	}
}

func TestSelectListDescriptionHiddenWhenNarrow(t *testing.T) {
	sl := NewSelectList(threeItems(), nil)
	lines := sl.Render(30)
	plain := StripAnsi(lines[1])
	if strings.Contains(plain, "first") {
		t.Errorf("description should be hidden at width=30: %q", plain)
	}
}

func TestSelectListMoveDown(t *testing.T) {
	sl := NewSelectList(threeItems(), nil)
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyDown})
	if sl.selected != 1 {
		t.Errorf("after down, selected=%d, want 1", sl.selected)
	}
}

func TestSelectListMoveUpWraps(t *testing.T) {
	sl := NewSelectList(threeItems(), nil)
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyUp})
	if sl.selected != 2 {
		t.Errorf("after up from 0, selected=%d, want 2 (wrap)", sl.selected)
	}
}

func TestSelectListMoveDownWraps(t *testing.T) {
	sl := NewSelectList(threeItems(), nil)
	sl.selected = 2
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyDown})
	if sl.selected != 0 {
		t.Errorf("after down from last, selected=%d, want 0 (wrap)", sl.selected)
	}
}

func TestSelectListEnterCallsOnSelect(t *testing.T) {
	var got SelectItem
	sl := NewSelectList(threeItems(), nil)
	sl.onSelect = func(item SelectItem) { got = item }
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyDown}) // move to Beta
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})
	if got.Value != "b" {
		t.Errorf("onSelect got %q, want 'b'", got.Value)
	}
}

func TestSelectListEscapeCallsOnCancel(t *testing.T) {
	called := false
	sl := NewSelectList(threeItems(), nil)
	sl.onCancel = func() { called = true }
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyEsc})
	if !called {
		t.Error("onCancel should have been called")
	}
}

func TestSelectListFilterByQuery(t *testing.T) {
	sl := NewSelectList(threeItems(), nil)
	// Type "be" to filter.
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if len(sl.filtered) != 1 {
		t.Fatalf("filter 'be' should match 1 item, got %d", len(sl.filtered))
	}
	if sl.filtered[0].Value != "b" {
		t.Errorf("filtered item should be Beta, got %q", sl.filtered[0].Value)
	}
}

func TestSelectListBackspaceClearsFilter(t *testing.T) {
	sl := NewSelectList(threeItems(), nil)
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}}) // no match
	if len(sl.filtered) != 0 {
		t.Fatalf("filter 'z' should match 0 items, got %d", len(sl.filtered))
	}
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyBackspace})
	if len(sl.filtered) != 3 {
		t.Errorf("after backspace, should show all 3 items, got %d", len(sl.filtered))
	}
}

func TestSelectListScrollIndicator(t *testing.T) {
	items := make([]SelectItem, 20)
	for i := range items {
		items[i] = SelectItem{Value: string(rune('a' + i)), Label: string(rune('A' + i))}
	}
	sl := NewSelectList(items, nil)
	sl.maxVisible = 5
	lines := sl.Render(80)
	// Last line should show scroll indicator.
	lastLine := StripAnsi(lines[len(lines)-1])
	if !strings.Contains(lastLine, "/20") {
		t.Errorf("scroll indicator should show /20: %q", lastLine)
	}
}

func TestSelectListDeleteConfirmation(t *testing.T) {
	deleted := ""
	sl := NewSelectList(threeItems(), nil)
	sl.onDelete = func(item SelectItem) { deleted = item.Value }
	// First 'd' enters confirmation.
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if sl.confirmDelete != 0 {
		t.Fatalf("confirmDelete should be 0 (first item), got %d", sl.confirmDelete)
	}
	if deleted != "" {
		t.Error("should not have deleted yet")
	}
	// Second 'd' confirms.
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if deleted != "a" {
		t.Errorf("should have deleted 'a', got %q", deleted)
	}
}

func TestSelectListDeleteCancelOnOtherKey(t *testing.T) {
	deleted := ""
	sl := NewSelectList(threeItems(), nil)
	sl.onDelete = func(item SelectItem) { deleted = item.Value }
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	// Cancel with any other key.
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if deleted != "" {
		t.Error("delete should have been cancelled")
	}
	if sl.confirmDelete != -1 {
		t.Errorf("confirmDelete should be -1 after cancel, got %d", sl.confirmDelete)
	}
}

func TestSelectListDeleteDisabledWhenNoCallback(t *testing.T) {
	sl := NewSelectList(threeItems(), nil)
	// 'd' with no onDelete should just filter (append to query).
	sl.HandleInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if sl.confirmDelete != -1 {
		t.Errorf("confirmDelete should remain -1 when onDelete is nil, got %d", sl.confirmDelete)
	}
	if sl.query != "d" {
		t.Errorf("query should be 'd' when delete is disabled, got %q", sl.query)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/tui/... -count=1 -run TestSelectList -v`
Expected: compilation error — `NewSelectList` is undefined.

---

### Task 2: SelectListComponent — Implementation

**Files:**
- Create: `gohome/internal/tui/selectlist.go`

**Step 1: Write the implementation**

```go
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var selectHighlight = lipgloss.NewStyle().Reverse(true)
var selectDeleteStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))

type SelectItem struct {
	Value       string
	Label       string
	Description string
}

type SelectListComponent struct {
	allItems      []SelectItem
	filtered      []SelectItem
	selected      int
	query         string
	maxVisible    int
	confirmDelete int
	onSelect      func(SelectItem)
	onCancel      func()
	onDelete      func(SelectItem)
}

func NewSelectList(items []SelectItem, onSelect func(SelectItem)) *SelectListComponent {
	sl := &SelectListComponent{
		allItems:      items,
		filtered:      append([]SelectItem{}, items...),
		maxVisible:    10,
		confirmDelete: -1,
		onSelect:      onSelect,
	}
	return sl
}

func (sl *SelectListComponent) Render(width int) []string {
	var lines []string

	// Search input line.
	lines = append(lines, "> "+sl.query+"\x1b[7m \x1b[0m")

	if len(sl.filtered) == 0 {
		lines = append(lines, "  (no matches)")
		return lines
	}

	// Viewport centering.
	total := len(sl.filtered)
	vis := sl.maxVisible
	if vis > total {
		vis = total
	}
	start := sl.selected - vis/2
	if start < 0 {
		start = 0
	}
	if start+vis > total {
		start = total - vis
	}
	if start < 0 {
		start = 0
	}
	end := start + vis

	showDesc := width > 40

	for i := start; i < end; i++ {
		item := sl.filtered[i]
		prefix := "   "
		if i == sl.selected {
			prefix = "-> "
		}

		label := item.Label
		line := prefix + label

		if showDesc && item.Description != "" {
			descMaxW := width - VisualWidth(line) - 2
			if descMaxW > 0 {
				desc := item.Description
				if VisualWidth(desc) > descMaxW {
					desc = TruncateText(desc, descMaxW)
				}
				padding := width - VisualWidth(line) - VisualWidth(desc)
				if padding < 1 {
					padding = 1
				}
				line = line + strings.Repeat(" ", padding) + desc
			}
		}

		// Confirmation state: show delete prompt.
		if sl.confirmDelete == i {
			line = selectDeleteStyle.Render(prefix + label + "  delete? d to confirm")
		} else if i == sl.selected {
			line = selectHighlight.Render(line)
		}

		lines = append(lines, line)
	}

	// Scroll indicator.
	if total > vis {
		lines = append(lines, fmt.Sprintf("  (%d/%d)", sl.selected+1, total))
	}

	return lines
}

func (sl *SelectListComponent) HandleInput(msg tea.KeyMsg) tea.Cmd {
	// Delete confirmation mode: second 'd' confirms, anything else cancels.
	if sl.confirmDelete >= 0 {
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == 'd' {
			if sl.onDelete != nil && sl.confirmDelete < len(sl.filtered) {
				sl.onDelete(sl.filtered[sl.confirmDelete])
			}
		}
		sl.confirmDelete = -1
		return nil
	}

	switch msg.Type {
	case tea.KeyUp:
		sl.selected--
		if sl.selected < 0 {
			sl.selected = len(sl.filtered) - 1
		}
		if sl.selected < 0 {
			sl.selected = 0
		}
	case tea.KeyDown:
		if len(sl.filtered) > 0 {
			sl.selected = (sl.selected + 1) % len(sl.filtered)
		}
	case tea.KeyEnter:
		if sl.onSelect != nil && sl.selected < len(sl.filtered) {
			sl.onSelect(sl.filtered[sl.selected])
		}
	case tea.KeyEsc:
		if sl.onCancel != nil {
			sl.onCancel()
		}
	case tea.KeyBackspace:
		if len(sl.query) > 0 {
			sl.query = sl.query[:len(sl.query)-1]
			sl.applyFilter()
		}
	case tea.KeyRunes:
		if len(msg.Runes) == 1 {
			r := msg.Runes[0]
			// 'd' triggers delete confirmation when onDelete is set and not filtering.
			if r == 'd' && sl.onDelete != nil && sl.query == "" {
				sl.confirmDelete = sl.selected
				return nil
			}
		}
		sl.query += string(msg.Runes)
		sl.applyFilter()
	}
	return nil
}

func (sl *SelectListComponent) applyFilter() {
	if sl.query == "" {
		sl.filtered = append([]SelectItem{}, sl.allItems...)
		sl.selected = 0
		return
	}
	q := strings.ToLower(sl.query)
	var out []SelectItem
	for _, item := range sl.allItems {
		if strings.Contains(strings.ToLower(item.Label), q) ||
			strings.Contains(strings.ToLower(item.Description), q) {
			out = append(out, item)
		}
	}
	sl.filtered = out
	sl.selected = 0
}

// SetItems replaces the item list and reapplies the current filter.
func (sl *SelectListComponent) SetItems(items []SelectItem) {
	sl.allItems = items
	sl.applyFilter()
}
```

**Step 2: Run tests to verify they pass**

Run: `go test ./gohome/internal/tui/... -count=1 -run TestSelectList -v`
Expected: all TestSelectList* tests PASS.

**Step 3: Commit**

```bash
git add gohome/internal/tui/selectlist.go gohome/internal/tui/selectlist_test.go
git commit -m "feat(tui): add SelectListComponent with filtering and delete confirm"
```

---

### Task 3: Cancellable Spinner — Tests

**Files:**
- Modify: `gohome/internal/tui/spinner_test.go`

**Step 1: Add new tests to the existing file**

Append the following tests to `spinner_test.go`. These tests are in **package `tui`** (internal), so they have direct access to the `onCancel` field.

```go
func TestSpinnerHandleInputEscCallsCancel(t *testing.T) {
	s := NewSpinner()
	s.Start("Working...")
	called := false
	s.SetOnCancel(func() { called = true })
	s.HandleInput(tea.KeyMsg{Type: tea.KeyEsc})
	if !called {
		t.Error("Escape should call onCancel")
	}
}

func TestSpinnerHandleInputEscNoopWithoutCallback(t *testing.T) {
	s := NewSpinner()
	s.Start("Working...")
	// No panic when onCancel is nil.
	s.HandleInput(tea.KeyMsg{Type: tea.KeyEsc})
}

func TestSpinnerHandleInputIgnoresOtherKeys(t *testing.T) {
	s := NewSpinner()
	s.Start("Working...")
	called := false
	s.SetOnCancel(func() { called = true })
	s.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})
	if called {
		t.Error("non-Escape keys should not trigger onCancel")
	}
}

func TestSpinnerRenderShowsCancelHint(t *testing.T) {
	s := NewSpinner()
	s.Start("Working...")
	s.SetOnCancel(func() {})
	lines := s.Render(80)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	plain := StripAnsi(lines[0])
	if !strings.Contains(plain, "Esc to cancel") {
		t.Errorf("expected cancel hint in %q", plain)
	}
}

func TestSpinnerRenderNoCancelHintWithoutCallback(t *testing.T) {
	s := NewSpinner()
	s.Start("Working...")
	lines := s.Render(80)
	plain := StripAnsi(lines[0])
	if strings.Contains(plain, "Esc to cancel") {
		t.Errorf("should not show cancel hint when onCancel is nil: %q", plain)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/tui/... -count=1 -run TestSpinner -v`
Expected: compilation error — `SetOnCancel` and `HandleInput` undefined on `*SpinnerComponent`.

---

### Task 4: Cancellable Spinner — Implementation

**Files:**
- Modify: `gohome/internal/tui/spinner.go`

**Step 1: Add the new fields and methods to `spinner.go`**

Add `onCancel` field to the struct, `SetOnCancel` method, `HandleInput` method, and update `Render` to show the hint. Add `ClearOnCancel` method for cleanup when spinner stops.

Changes to make:

1. Add `onCancel func()` field to `SpinnerComponent` struct (after `message string`).

2. Add these methods:

```go
func (s *SpinnerComponent) SetOnCancel(fn func()) {
	s.onCancel = fn
}

func (s *SpinnerComponent) ClearOnCancel() {
	s.onCancel = nil
}

func (s *SpinnerComponent) HandleInput(msg tea.KeyMsg) tea.Cmd {
	if msg.Type == tea.KeyEsc && s.onCancel != nil {
		s.onCancel()
	}
	return nil
}
```

3. Update the `Render` method: after building the line `spinnerStyle.Render(frame) + " " + s.message`, check if `s.onCancel != nil`. If so, append `"  " + lipgloss.NewStyle().Faint(true).Render("(Esc to cancel)")`.

4. Update `Stop()` to also call `s.ClearOnCancel()` so the hint disappears when the spinner stops.

**Step 2: Run tests to verify they pass**

Run: `go test ./gohome/internal/tui/... -count=1 -run TestSpinner -v`
Expected: all TestSpinner* tests PASS.

**Step 3: Commit**

```bash
git add gohome/internal/tui/spinner.go gohome/internal/tui/spinner_test.go
git commit -m "feat(tui): add Escape-to-cancel to SpinnerComponent"
```

---

### Task 5: Wire Spinner Cancel into Model

**Files:**
- Modify: `gohome/internal/tui/model.go`

**Step 1: Add the cancel wiring**

In `handleAgentEvent`, where the spinner starts (the `switch ev.Kind` block around line 332), set the cancel callback:

```go
case agent.EventThinkingDelta:
	if !m.spinner.Active() {
		m.spinner.Start("Thinking...")
		m.spinner.SetOnCancel(m.cancelFocusedSession)
	}
case agent.EventTokenDelta:
	if !m.spinner.Active() {
		m.spinner.Start("Generating...")
		m.spinner.SetOnCancel(m.cancelFocusedSession)
	} else {
		m.spinner.SetMessage("Generating...")
	}
```

Add the `cancelFocusedSession` helper method to `Model`:

```go
func (m *Model) cancelFocusedSession() {
	if m.slashCB.CancelSession != nil {
		m.slashCB.CancelSession(m.focused)
	}
	sv := m.sessions[m.focused]
	if sv != nil {
		sv.InFlight = false
		sv.Timeline = append(sv.Timeline, TimelineEntry{Kind: "notice", Text: "Cancelled."})
	}
	m.pendingMessages = m.pendingMessages[:0]
	m.spinner.Stop()
	m.statusMsg = "Cancelled"
	m.rebuildViewport()
}
```

In `Update()`, route Escape to the spinner when it is active and no overlay is showing. In the `tea.KeyMsg` switch, before the `switch msg.Type` block (around line 557), add:

```go
// Route Escape to cancellable spinner when active and no overlay is open.
if msg.Type == tea.KeyEsc && m.spinner.Active() &&
	!m.browsing && !m.selectingModel {
	m.spinner.HandleInput(msg)
	return m, tea.Batch(cmds...)
}
```

**Step 2: Run all tests**

Run: `go test ./gohome/internal/tui/... -count=1 -v`
Expected: all tests PASS.

**Step 3: Commit**

```bash
git add gohome/internal/tui/model.go
git commit -m "feat(tui): wire Escape-to-cancel spinner into Model update loop"
```

---

### Task 6: Session Browser — Tests

**Files:**
- Create: `gohome/internal/tui/session_browser_test.go`

**Step 1: Write the tests**

```go
package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

func sampleListings() []session.Listing {
	now := time.Now()
	return []session.Listing{
		{
			ID:         "abc123",
			Title:      "fix the login bug",
			StartedAt:  now.Add(-2 * time.Hour),
			LastActive: now.Add(-1 * time.Hour),
			Path:       "/tmp/test-abc123.jsonl",
		},
		{
			ID:         "def456",
			Title:      "",
			StartedAt:  now.Add(-48 * time.Hour),
			LastActive: now.Add(-24 * time.Hour),
			Path:       "/tmp/test-def456.jsonl",
		},
		{
			ID:         "ghi789",
			Title:      "refactor the TUI renderer",
			StartedAt:  now.Add(-168 * time.Hour),
			LastActive: now.Add(-72 * time.Hour),
			Path:       "/tmp/test-ghi789.jsonl",
		},
	}
}

func TestSessionBrowserRender(t *testing.T) {
	sb := NewSessionBrowser(sampleListings())
	lines := sb.Render(80)
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines (search + 3 items), got %d", len(lines))
	}
	joined := strings.Join(lines, "\n")
	plainJoined := StripAnsi(joined)
	if !strings.Contains(plainJoined, "fix the login bug") {
		t.Error("should show session title")
	}
}

func TestSessionBrowserUsesIDWhenNoTitle(t *testing.T) {
	sb := NewSessionBrowser(sampleListings())
	lines := sb.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "def456") {
		t.Error("should fall back to session ID when title is empty")
	}
}

func TestSessionBrowserSelectReturnsID(t *testing.T) {
	var selectedID string
	sb := NewSessionBrowser(sampleListings())
	sb.SetOnSelect(func(id string) { selectedID = id })
	sb.list.HandleInput(tea.KeyMsg{Type: tea.KeyDown})
	sb.list.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})
	if selectedID != "def456" {
		t.Errorf("expected 'def456', got %q", selectedID)
	}
}

func TestSessionBrowserCancel(t *testing.T) {
	cancelled := false
	sb := NewSessionBrowser(sampleListings())
	sb.SetOnCancel(func() { cancelled = true })
	sb.list.HandleInput(tea.KeyMsg{Type: tea.KeyEsc})
	if !cancelled {
		t.Error("Escape should call onCancel")
	}
}

func TestSessionBrowserRelativeTime(t *testing.T) {
	sb := NewSessionBrowser(sampleListings())
	lines := sb.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "ago") {
		t.Errorf("should show relative time: %s", joined)
	}
}

func TestRelativeTime(t *testing.T) {
	tests := []struct {
		dur    time.Duration
		expect string
	}{
		{30 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{2 * time.Hour, "2h ago"},
		{48 * time.Hour, "2d ago"},
		{14 * 24 * time.Hour, "2w ago"},
	}
	now := time.Now()
	for _, tt := range tests {
		got := relativeTime(now.Add(-tt.dur))
		if got != tt.expect {
			t.Errorf("relativeTime(%v) = %q, want %q", tt.dur, got, tt.expect)
		}
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/tui/... -count=1 -run "TestSessionBrowser|TestRelativeTime" -v`
Expected: compilation error — `NewSessionBrowser`, `relativeTime` undefined.

---

### Task 7: Session Browser — Implementation

**Files:**
- Create: `gohome/internal/tui/session_browser.go`

**Step 1: Write the implementation**

```go
package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

type SessionBrowserComponent struct {
	list     *SelectListComponent
	listings []session.Listing
}

func NewSessionBrowser(listings []session.Listing) *SessionBrowserComponent {
	items := make([]SelectItem, len(listings))
	for i, l := range listings {
		label := l.Title
		if label == "" {
			label = l.ID
		}
		if len([]rune(label)) > 40 {
			label = string([]rune(label)[:40])
		}
		items[i] = SelectItem{
			Value:       l.ID,
			Label:       label,
			Description: relativeTime(l.LastActive),
		}
	}
	sb := &SessionBrowserComponent{
		list:     NewSelectList(items, nil),
		listings: listings,
	}
	return sb
}

func (sb *SessionBrowserComponent) SetOnSelect(fn func(id string)) {
	sb.list.onSelect = func(item SelectItem) {
		fn(item.Value)
	}
}

func (sb *SessionBrowserComponent) SetOnCancel(fn func()) {
	sb.list.onCancel = fn
}

func (sb *SessionBrowserComponent) SetOnDelete(fn func(listing session.Listing)) {
	sb.list.onDelete = func(item SelectItem) {
		for _, l := range sb.listings {
			if l.ID == item.Value {
				fn(l)
				break
			}
		}
		// Remove from listings and rebuild items.
		var remaining []session.Listing
		for _, l := range sb.listings {
			if l.ID != item.Value {
				remaining = append(remaining, l)
			}
		}
		sb.listings = remaining
		items := make([]SelectItem, len(remaining))
		for i, l := range remaining {
			label := l.Title
			if label == "" {
				label = l.ID
			}
			if len([]rune(label)) > 40 {
				label = string([]rune(label)[:40])
			}
			items[i] = SelectItem{
				Value:       l.ID,
				Label:       label,
				Description: relativeTime(l.LastActive),
			}
		}
		sb.list.SetItems(items)
	}
}

func (sb *SessionBrowserComponent) Render(width int) []string {
	return sb.list.Render(width)
}

func (sb *SessionBrowserComponent) HandleInput(msg tea.KeyMsg) tea.Cmd {
	return sb.list.HandleInput(msg)
}

func relativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw ago", int(d.Hours()/(24*7)))
	}
}
```

**Step 2: Run tests**

Run: `go test ./gohome/internal/tui/... -count=1 -run "TestSessionBrowser|TestRelativeTime" -v`
Expected: all PASS.

**Step 3: Commit**

```bash
git add gohome/internal/tui/session_browser.go gohome/internal/tui/session_browser_test.go
git commit -m "feat(tui): add SessionBrowserComponent wrapping SelectList"
```

---

### Task 8: Model Selector — Tests

**Files:**
- Create: `gohome/internal/tui/model_selector_test.go`

**Step 1: Write the tests**

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/config"
)

func sampleEndpoints() map[string]config.Endpoint {
	return map[string]config.Endpoint{
		"anthropic": {
			Wire:         config.WireAnthropic,
			DefaultModel: "claude-sonnet-4-20250514",
		},
		"openai": {
			Wire:         config.WireOpenAI,
			DefaultModel: "gpt-4o",
		},
	}
}

func TestModelSelectorRender(t *testing.T) {
	ms := NewModelSelector(sampleEndpoints(), "anthropic")
	lines := ms.Render(80)
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines (search + 2 items), got %d", len(lines))
	}
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "anthropic") {
		t.Error("should show 'anthropic' endpoint")
	}
}

func TestModelSelectorCurrentFirst(t *testing.T) {
	ms := NewModelSelector(sampleEndpoints(), "openai")
	lines := ms.Render(80)
	// First item line (after search) should contain "openai".
	firstItem := StripAnsi(lines[1])
	if !strings.Contains(firstItem, "openai") {
		t.Errorf("current endpoint should be listed first: %q", firstItem)
	}
}

func TestModelSelectorCurrentMarked(t *testing.T) {
	ms := NewModelSelector(sampleEndpoints(), "anthropic")
	lines := ms.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "(current)") {
		t.Error("current endpoint should be marked with (current)")
	}
}

func TestModelSelectorSelectReturnsModelName(t *testing.T) {
	var gotEndpoint, gotModel string
	ms := NewModelSelector(sampleEndpoints(), "anthropic")
	ms.SetOnSelect(func(endpoint, model string) {
		gotEndpoint = endpoint
		gotModel = model
	})
	// Move to second item (non-current) and select.
	ms.list.HandleInput(tea.KeyMsg{Type: tea.KeyDown})
	ms.list.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})
	if gotEndpoint == "" || gotModel == "" {
		t.Errorf("expected endpoint and model, got endpoint=%q model=%q", gotEndpoint, gotModel)
	}
}

func TestModelSelectorCancel(t *testing.T) {
	cancelled := false
	ms := NewModelSelector(sampleEndpoints(), "anthropic")
	ms.SetOnCancel(func() { cancelled = true })
	ms.list.HandleInput(tea.KeyMsg{Type: tea.KeyEsc})
	if !cancelled {
		t.Error("Escape should call onCancel")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/tui/... -count=1 -run TestModelSelector -v`
Expected: compilation error — `NewModelSelector` undefined.

---

### Task 9: Model Selector — Implementation

**Files:**
- Create: `gohome/internal/tui/model_selector.go`

**Step 1: Write the implementation**

```go
package tui

import (
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/config"
)

type ModelSelectorComponent struct {
	list      *SelectListComponent
	endpoints map[string]config.Endpoint
}

func NewModelSelector(endpoints map[string]config.Endpoint, currentEndpoint string) *ModelSelectorComponent {
	// Sort endpoint names alphabetically, but put current first.
	names := make([]string, 0, len(endpoints))
	for name := range endpoints {
		if name != currentEndpoint {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if _, ok := endpoints[currentEndpoint]; ok {
		names = append([]string{currentEndpoint}, names...)
	}

	items := make([]SelectItem, len(names))
	for i, name := range names {
		ep := endpoints[name]
		desc := ep.DefaultModel
		if name == currentEndpoint {
			desc += " (current)"
		}
		items[i] = SelectItem{
			Value:       name,
			Label:       name,
			Description: desc,
		}
	}

	ms := &ModelSelectorComponent{
		list:      NewSelectList(items, nil),
		endpoints: endpoints,
	}
	return ms
}

func (ms *ModelSelectorComponent) SetOnSelect(fn func(endpoint, model string)) {
	ms.list.onSelect = func(item SelectItem) {
		ep := ms.endpoints[item.Value]
		fn(item.Value, ep.DefaultModel)
	}
}

func (ms *ModelSelectorComponent) SetOnCancel(fn func()) {
	ms.list.onCancel = fn
}

func (ms *ModelSelectorComponent) Render(width int) []string {
	return ms.list.Render(width)
}

func (ms *ModelSelectorComponent) HandleInput(msg tea.KeyMsg) tea.Cmd {
	return ms.list.HandleInput(msg)
}
```

**Step 2: Run tests**

Run: `go test ./gohome/internal/tui/... -count=1 -run TestModelSelector -v`
Expected: all PASS.

**Step 3: Commit**

```bash
git add gohome/internal/tui/model_selector.go gohome/internal/tui/model_selector_test.go
git commit -m "feat(tui): add ModelSelectorComponent wrapping SelectList"
```

---

### Task 10: Wire Session Browser into Model

**Files:**
- Modify: `gohome/internal/tui/model.go`
- Modify: `gohome/internal/tui/slash.go`

**Step 1: Add new fields to Model struct**

Add these fields to the `Model` struct (after `fileSearching bool`):

```go
homeDir        string
cwd            string
sessionBrowser *SessionBrowserComponent
browsing       bool
```

**Step 2: Add setter methods**

Add to `model.go`:

```go
func (m *Model) SetHomeDir(dir string) {
	m.homeDir = dir
}

func (m *Model) SetCWD(dir string) {
	m.cwd = dir
}
```

**Step 3: Add `ListSessions` to `SlashCallbacks`**

In `slash.go`, add a new field:

```go
type SlashCallbacks struct {
	NewSession    func() (string, error)
	ResumeSession func(id string) error
	CancelSession func(id string)
	SetModel      func(name string) error
	ListSessions  func() ([]session.Listing, error)  // NEW
}
```

Add the import for `"github.com/jhyoong/GoHome/gohome/internal/session"` to `slash.go`.

**Step 4: Add `/sessions` to the slash command list and handler**

In `model.go`, update `slashCommands`:

```go
var slashCommands = []string{
	"/new", "/resume", "/sessions", "/yolo", "/endpoint", "/model", "/cancel", "/tokens", "/quit",
}
```

Add the `/sessions` case to `handleSlashCommand` (before `default:`):

```go
case "/sessions":
	if m.slashCB.ListSessions == nil {
		m.statusMsg = "/sessions: not configured"
		break
	}
	listings, err := m.slashCB.ListSessions()
	if err != nil {
		m.statusMsg = fmt.Sprintf("/sessions: %v", err)
		break
	}
	if len(listings) == 0 {
		m.statusMsg = "No sessions found"
		break
	}
	sb := NewSessionBrowser(listings)
	sb.SetOnSelect(func(id string) {
		m.browsing = false
		m.sessionBrowser = nil
		if m.slashCB.ResumeSession != nil {
			if err := m.slashCB.ResumeSession(id); err != nil {
				m.statusMsg = fmt.Sprintf("/sessions: %v", err)
				return
			}
		}
		m.getOrCreateSession(id, 0)
		m.focused = id
		m.cursor = 0
		m.statusMsg = "Resumed: " + id
		m.rebuildViewport()
	})
	sb.SetOnCancel(func() {
		m.browsing = false
		m.sessionBrowser = nil
	})
	sb.SetOnDelete(func(l session.Listing) {
		_ = os.Remove(l.Path)
		m.statusMsg = "Deleted session: " + l.ID
	})
	m.sessionBrowser = sb
	m.browsing = true
```

Add import for `"os"` and `"github.com/jhyoong/GoHome/gohome/internal/session"` to `model.go` if not present.

**Step 5: Route input to session browser in Update()**

In the `tea.KeyMsg` handler, after the `showTokens` block (around line 555), add:

```go
if m.browsing && m.sessionBrowser != nil {
	m.sessionBrowser.HandleInput(msg)
	return m, tea.Batch(cmds...)
}
```

**Step 6: Render session browser in View()**

In `View()`, replace the editor rendering section (the `// Input region` block) with logic that checks `m.browsing`:

```go
// Input region (swappable slot).
if m.activeApproval != nil {
	sections = append(sections, renderApprovalOverlay(m.activeApproval, m.winW))
} else if m.browsing && m.sessionBrowser != nil {
	browserLines := m.sessionBrowser.Render(m.winW)
	sections = append(sections, strings.Join(browserLines, "\n"))
} else if m.selectingModel && m.modelSelector != nil {
	selectorLines := m.modelSelector.Render(m.winW)
	sections = append(sections, strings.Join(selectorLines, "\n"))
} else {
	palette := m.slashPalette()
	if palette != "" {
		sections = append(sections, palette)
	}
	m.editor.SetTermHeight(m.winH)
	editorLines := m.editor.Render(m.winW)
	sections = append(sections, strings.Join(editorLines, "\n"))
}
```

**Step 7: Run all tests**

Run: `go test ./gohome/internal/tui/... -count=1 -v`
Expected: all tests PASS.

**Step 8: Commit**

```bash
git add gohome/internal/tui/model.go gohome/internal/tui/slash.go
git commit -m "feat(tui): wire /sessions command to session browser"
```

---

### Task 11: Wire Model Selector into Model

**Files:**
- Modify: `gohome/internal/tui/model.go`

**Step 1: Add new fields to Model struct**

Add these fields (after `browsing bool`):

```go
settings       config.Settings
modelSelector  *ModelSelectorComponent
selectingModel bool
```

Add import for `"github.com/jhyoong/GoHome/gohome/internal/config"`.

**Step 2: Add setter method**

```go
func (m *Model) SetSettings(s config.Settings) {
	m.settings = s
}
```

**Step 3: Update the `/model` case in `handleSlashCommand`**

Replace the existing `/model` case:

```go
case "/model":
	if len(fields) >= 2 {
		// Direct model set: /model <name>
		name := fields[1]
		if m.slashCB.SetModel != nil {
			err := m.slashCB.SetModel(name)
			if err != nil {
				m.statusMsg = fmt.Sprintf("/model: %v", err)
			} else {
				m.modelName = name
				m.statusMsg = "Model set to " + name
			}
		} else {
			m.statusMsg = "/model: not configured"
		}
		break
	}
	// No args: open model selector.
	if len(m.settings.Endpoints) == 0 {
		m.statusMsg = fmt.Sprintf("Current model: %s", m.modelName)
		break
	}
	ms := NewModelSelector(m.settings.Endpoints, m.settings.DefaultEndpoint)
	ms.SetOnSelect(func(endpoint, model string) {
		m.selectingModel = false
		m.modelSelector = nil
		if m.slashCB.SetModel != nil {
			if err := m.slashCB.SetModel(model); err != nil {
				m.statusMsg = fmt.Sprintf("/model: %v", err)
				return
			}
		}
		m.modelName = model
		m.settings.DefaultEndpoint = endpoint
		m.statusMsg = "Model set to " + model
	})
	ms.SetOnCancel(func() {
		m.selectingModel = false
		m.modelSelector = nil
	})
	m.modelSelector = ms
	m.selectingModel = true
```

**Step 4: Route input to model selector in Update()**

In the `tea.KeyMsg` handler, after the browsing check, add:

```go
if m.selectingModel && m.modelSelector != nil {
	m.modelSelector.HandleInput(msg)
	return m, tea.Batch(cmds...)
}
```

**Step 5: The View() changes were already made in Task 10 Step 6**

The `View()` method already has the `m.selectingModel` branch from Task 10. Verify it is present.

**Step 6: Run all tests**

Run: `go test ./gohome/internal/tui/... -count=1 -v`
Expected: all tests PASS.

**Step 7: Commit**

```bash
git add gohome/internal/tui/model.go
git commit -m "feat(tui): wire /model selector into Model"
```

---

### Task 12: Integration Tests

**Files:**
- Create: `gohome/internal/tui/integration_test.go`

**Step 1: Write integration tests using teatest**

```go
package tui_test

import (
	"bytes"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/session"
	"github.com/jhyoong/GoHome/gohome/internal/tui"
)

func TestSlashSessionsOpensAndCloses(t *testing.T) {
	m := tui.New(nil, "")
	m.SetSlashCallbacks(tui.SlashCallbacks{
		ListSessions: func() ([]session.Listing, error) {
			return []session.Listing{
				{ID: "s1", Title: "test session"},
			}, nil
		},
	})

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Type("/sessions")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Session browser should appear with the session title.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("test session"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Escape closes the browser.
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})

	// Editor border should reappear.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

func TestSlashModelOpensSelector(t *testing.T) {
	m := tui.New(nil, "")
	m.SetSettings(config.Settings{
		Endpoints: map[string]config.Endpoint{
			"anthropic": {DefaultModel: "claude-sonnet-4-20250514"},
			"openai":    {DefaultModel: "gpt-4o"},
		},
		DefaultEndpoint: "anthropic",
	})

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Type("/model")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Model selector should show endpoint names.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("anthropic"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Escape closes.
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
```

**Step 2: Run integration tests**

Run: `go test ./gohome/internal/tui/... -count=1 -run "TestSlashSessions|TestSlashModel" -v`
Expected: all PASS.

**Step 3: Commit**

```bash
git add gohome/internal/tui/integration_test.go
git commit -m "test(tui): add integration tests for session browser and model selector"
```

---

### Task 13: Run Full Test Suite and Verify

**Step 1: Run all tests**

Run: `go test ./gohome/internal/tui/... -count=1 -v`
Expected: all tests PASS, no regressions.

**Step 2: Run the full project test suite**

Run: `go test ./... -count=1`
Expected: all packages PASS.

**Step 3: Verify the build compiles**

Run: `go build ./...`
Expected: clean build, no errors.

Plan complete and saved to `docs/plans/2026-06-06-session-model-loader-impl.md`. Two execution options:

**1. Subagent-Driven (this session)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Parallel Session (separate)** — Open a new session with executing-plans, batch execution with checkpoints.

Which approach?