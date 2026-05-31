# TUI P1 Design -- Experience Polish

Addresses 4 remaining P1 gaps from `docs/tui-gap-analysis.md` (items 7, 8, 10, 11).
P1 items 6 (ANSI-aware truncation) and 9 (word wrapping) were resolved in P0.

---

## 1. Tool Execution Rich Display (Gap #7)

### Data model

Add `Status string` to `TimelineEntry`. Valid values: `""`, `"pending"`, `"success"`, `"error"`.

### Event flow

- `EventToolCallDone`: create tool entry with `Status = "pending"`.
- `EventToolResult`: set `ToolResult` on the matching entry. Set `Status = "success"` normally, `"error"` when the result indicates failure.

### Rendering

Replace the single `toolStyle` with three status-aware styles:

| Status | Color | Notes |
|--------|-------|-------|
| pending | Yellow (3), italic | Current appearance |
| success | Green (2) | Indicates completion |
| error | Red (1), bold | Prefixes result with "ERROR:" |

Expand/collapse via Enter is unchanged.

### Theme

Add `ToolPending`, `ToolSuccess`, `ToolError` styles to `style.Theme`, replacing `ToolLine`.

### Files

- `model.go`: TimelineEntry struct, handleAgentEvent status assignment
- `chat.go`: renderToolLine status-aware color selection
- `style/style.go`: 3 new styles replacing ToolLine

---

## 2. Bracketed Paste Support (Gap #8)

### Problem

Without bracketed paste, pasting multi-line content triggers Enter handling on each `\n`, causing accidental submissions.

### Solution

Wrap `os.Stdin` with a `PasteReader` that detects paste bracket sequences.

1. Enable bracketed paste: write `\x1b[?2004h` on TUI start, `\x1b[?2004l` on exit.
2. Detect `\x1b[200~` (paste start) in the byte stream.
3. Buffer all bytes until `\x1b[201~` (paste end).
4. Emit `pasteMsg{Text string}` with markers stripped.

### Paste handling

On `pasteMsg` in `Model.Update`:
- Strip `\r` characters.
- Replace tabs with 4 spaces.
- Insert via `EditorComponent.InsertText(s)` -- splits on `\n`, appends first line at cursor, creates new lines for the rest, cursor ends at end of last inserted line.

### Integration

Use `tea.WithInput(pasteReader)` when creating `tea.Program`.

### Files

- `paste.go` (new): PasteReader struct, pasteMsg type (~60 lines)
- `model.go`: handle pasteMsg, enable/disable bracketed paste
- `editor.go`: add InsertText method
- `frontend.go` or program setup: wire PasteReader

---

## 3. External Editor Support (Gap #10)

### Trigger

`Ctrl+E` keybinding (when no approval active, no token overlay).

### Flow

1. Write editor content to temp file (`os.CreateTemp("", "gohome-*.md")`).
2. Determine editor: `$VISUAL` > `$EDITOR` > `"vi"`.
3. Use `tea.ExecProcess` to suspend TUI and launch the editor.
4. On exit, read temp file content back.
5. Set editor content via `EditorComponent.SetValue(content)`.
6. Delete temp file.

### Message type

`externalEditorMsg{Content string, Err error}`

### Error handling

If the editor exits with an error or the file cannot be read, set `m.statusMsg` to the error string. Editor content is not modified on failure.

### Files

- `model.go`: Ctrl+E handler, externalEditorMsg handler (~30 lines)

---

## 4. Consistent Context Bar Thresholds (Gap #11)

### Problem

Progress bar colors (green/yellow/red at 50%/80%) do not match context warning thresholds (80%/95%).

### Fix

Align `progressBar()` thresholds to match warnings:

| Range | Color | Matches |
|-------|-------|---------|
| 0-80% | Green (2) | No warning |
| 80-95% | Yellow (3) | 80% warning |
| >95% | Red (1) | 95% warning |

### Files

- `progress.go`: change threshold constants (2-line change)
- `progress_test.go`: update threshold assertions

---

## Testing Strategy

- Tool display: test renderToolLine output for each status in `chat_test.go` or `tool_test.go`.
- Bracketed paste: unit test PasteReader with synthetic bracket sequences. Test EditorComponent.InsertText with multi-line content.
- External editor: test the message flow (externalEditorMsg handling). The actual editor launch is integration-only.
- Thresholds: update existing progressBar table-driven tests.
