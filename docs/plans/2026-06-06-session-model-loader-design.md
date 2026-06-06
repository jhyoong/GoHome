# P2 Features Design: Session Browser, Model Selector, Cancellable Spinner

Three features from the TUI gap analysis P2 list (#16, #17, #18). All share a
common `SelectListComponent` primitive.

---

## 1. SelectListComponent (Shared Primitive)

**File:** `internal/tui/selectlist.go`

### Types

```go
type SelectItem struct {
    Value       string   // unique identifier
    Label       string   // display text (primary column)
    Description string   // secondary text (right side, shown if width > 40)
}

type SelectListComponent struct {
    allItems       []SelectItem
    filtered       []SelectItem
    selected       int
    query          string
    maxVisible     int
    confirmDelete  int   // -1 = not confirming, else index
    onSelect       func(SelectItem)
    onCancel       func()
    onDelete       func(SelectItem) // optional, nil disables delete
}
```

### Interface

Implements `Component` + `Interactive`.

### Rendering

- Top line: `> {query}` with cursor (search input).
- Below: visible slice of filtered items, up to `maxVisible` (default 10).
- Selected item: `-> {label}` in highlight. Others: `   {label}`.
- If `width > 40` and item has Description, show it right-aligned.
- Scroll indicator `(N/M)` when items exceed maxVisible.
- Viewport centering: `startIndex = max(0, min(selected - maxVisible/2, total - maxVisible))`.

### Filtering

Case-insensitive substring match on Label and Description. All printable
characters append to query, Backspace removes last character. Re-filter resets
selection to 0.

### Key Handling

- Up/Down: move selection with wrap-around.
- Enter: call `onSelect(filtered[selected])`.
- Escape: call `onCancel()`.
- `d`: enter delete confirmation (item turns red, shows "delete? d to confirm").
  Second `d` calls `onDelete(item)`. Any other key cancels confirmation.
- Printable chars: append to filter query, re-filter.
- Backspace: remove last query char, re-filter.

---

## 2. Session Browser

**File:** `internal/tui/session_browser.go`

### Trigger

`/sessions` slash command. Replaces the editor area. Escape or selection
restores the editor.

### Data Source

`session.List(home, cwd)` returns `[]Listing` sorted by most recent first.

### New Fields on Model

```go
homeDir        string     // set during construction
cwd            string     // set during construction
sessionBrowser *SessionBrowserComponent
browsing       bool
```

### SessionBrowserComponent

```go
type SessionBrowserComponent struct {
    list     *SelectListComponent
    listings []session.Listing
}
```

Wraps `SelectListComponent`. Converts each `Listing` into a `SelectItem`:
- `Value`: session ID.
- `Label`: title (or ID if no title), truncated to 40 chars.
- `Description`: relative time (e.g. "2h ago", "3d ago").

### Delete Flow

- Press `d`: item turns red, shows confirmation prompt.
- Press `d` again: deletes session JSONL file via `os.Remove(listing.Path)`,
  removes from list.
- Any other key: cancels confirmation.
- Handled via `SelectListComponent.onDelete` callback.

### Integration

- `/sessions` handler: calls `session.List()`, builds items, creates component,
  sets `m.browsing = true`.
- `View()`: when `m.browsing`, render browser lines instead of editor.
- `Update()`: when `m.browsing`, route keys to browser.
- On select: calls `m.slashCB.ResumeSession(id)`, sets focused session,
  restores editor.
- On cancel (Escape): restores editor, clears browsing state.

---

## 3. Model Selector

**File:** `internal/tui/model_selector.go`

### Trigger

`/model` with no arguments opens the selector. `/model <name>` retains existing
behavior (set model directly).

### Data Source

`config.Settings.Endpoints` map. Each endpoint has a name (map key) and
`DefaultModel`.

### New Fields on Model

```go
settings       config.Settings
modelSelector  *ModelSelectorComponent
selectingModel bool
```

### ModelSelectorComponent

```go
type ModelSelectorComponent struct {
    list *SelectListComponent
}
```

Converts each endpoint to a `SelectItem`:
- `Value`: endpoint name (map key).
- `Label`: endpoint name.
- `Description`: default model name. Current endpoint marked with `(current)`.

Current endpoint listed first.

### Integration

- `/model` (no args): builds items from `m.settings.Endpoints`, creates
  component, sets `m.selectingModel = true`.
- `View()`: when `m.selectingModel`, render selector instead of editor.
- `Update()`: when `m.selectingModel`, route keys to selector.
- On select: calls `m.slashCB.SetModel(item.Description)`, updates
  `m.modelName`, restores editor.
- On cancel: restores editor.

---

## 4. Cancellable Spinner

**File:** `internal/tui/spinner.go` (modify existing)

### Changes to SpinnerComponent

```go
type SpinnerComponent struct {
    frame    int
    active   bool
    message  string
    onCancel func()  // called when Escape is pressed during spin
}
```

### New Method

```go
func (s *SpinnerComponent) HandleInput(msg tea.KeyMsg) tea.Cmd {
    if msg.Type == tea.KeyEsc && s.onCancel != nil {
        s.onCancel()
    }
    return nil
}
```

`SpinnerComponent` now implements `Interactive` in addition to `Component`.

### Cancel Callback

Set when the spinner starts in `handleAgentEvent`. The callback performs the
same cancellation logic as Ctrl+C single-press:

1. Calls `slashCB.CancelSession(focusedSessionID)`.
2. Sets `sv.InFlight = false`.
3. Appends a "Cancelled." notice to the timeline.
4. Clears pending messages.
5. Stops the spinner.

### Rendering Enhancement

When `onCancel` is non-nil, the rendered line appends `" (Esc to cancel)"` in
dim text.

### Integration with Model.Update()

When the spinner is active and no overlay (approval, browser, selector) is open,
Escape is routed to `m.spinner.HandleInput(msg)` before reaching the editor.

---

## 5. Render Order

```
session strip
notification line
chat area
spinner              <-- cancellable via Esc
file search popup
pending messages
status message
editor / browser / model selector / approval   <-- swappable slot
status bar
```

Only one of {editor, session browser, model selector, approval} occupies the
slot at a time.

---

## 6. New Files

| File | Purpose |
|---|---|
| `selectlist.go` | Generic scrollable filterable list component |
| `selectlist_test.go` | Tests for SelectListComponent |
| `session_browser.go` | Session browser wrapper |
| `session_browser_test.go` | Tests for session browser |
| `model_selector.go` | Model selector wrapper |
| `model_selector_test.go` | Tests for model selector |

Modified files: `spinner.go`, `spinner_test.go`, `model.go`, `slash.go`.
