# Subagent Read-Only Mode & Scroll Fix Design

## Problem Statement

Two TUI issues:

1. **Subagent sessions accept input after completion.** When a subagent finishes (sends response back to parent), the session tab still allows typing. There is no distinction between "done" and "cancelled" at the TUI layer -- both just set `InFlight = false`.

2. **Scrolling breaks with large content.** `WrapText` does not handle newlines in input. Tool results containing `\n` produce `[]string` entries with embedded newlines. The terminal renders these as multiple visual lines, but the scroll math (`entryLineCount`, `countLines`) counts them as single lines. This makes the scroll range too short and causes content to overflow or be unreachable via PgUp.

## Design

### Issue 1: Subagent Session Read-Only Mode

**Approach:** Add a `Completed` flag to `SessionView`, populated via an end-reason field on `EventSessionEnded`. Only normal completion ("done") sets `Completed = true`; cancellation does not.

#### Event layer (`internal/agent/events.go`)

Add `EndReason string` to the `Event` struct. Values: `"done"` (normal completion) or `"cancelled"` (context cancelled / manual cancel).

#### Emit site (`internal/agent/spawn.go`)

The existing `EventSessionEnded` emission computes `endReason` after the emit. Move the reason computation before the emit and populate `Event.EndReason`.

#### TUI model (`internal/tui/model.go`)

Add `Completed bool` to `SessionView`.

#### TUI event handler (`internal/tui/model_agent.go`)

In the `EventSessionEnded` case, check `ev.EndReason == "done"` and set `sv.Completed = true`. Cancelled sessions stay `Completed = false` (still interactive).

#### Input gating (`internal/tui/model_keys.go`)

In the Enter key handler, before checking `sv.InFlight`, check `sv.Completed`. If true, set `m.statusMsg = "Session complete"` and reject input. Navigation keys (arrows, PgUp/PgDn, Enter-to-expand, 'c' to copy) remain ungated.

#### View (`internal/tui/view.go`)

When rendering the editor area for a focused session where `sv.Completed == true`, render a dim `[Session complete]` label instead of the editor widget.

### Issue 2: Fix WrapText Newline Handling

**Approach:** Fix `WrapText` at the source so all call sites automatically handle newlines correctly.

#### WrapText refactor (`internal/tui/ansi.go`)

Split the input on `\n` at the start of `WrapText`. For each resulting segment, run the existing wrapping logic independently. Concatenate all wrapped results into a single `[]string` return value.

Implementation: extract the current wrapping body into a helper (`wrapSingleLine`) that handles one newline-free segment. `WrapText` becomes a loop over `strings.Split(s, "\n")`, calling `wrapSingleLine` for each and tracking the active SGR state across segments.

No changes needed to `entryLineCount`, `countLines`, or `Render` -- they already count `len(lines)` from `WrapText`. Once `WrapText` returns the correct number of lines, the scroll math is automatically accurate.

#### PgUp/PgDn scroll amount (`internal/tui/model_keys.go`)

Change the hardcoded `5` in `KeyPgUp`/`KeyPgDown` handlers to `m.chat.maxHeight - 2` (full-page scroll with 2 lines of overlap).

#### Tests (`internal/tui/ansi_test.go`)

Add test cases for `WrapText` with:
- Single newline in middle of text
- Multiple consecutive newlines (empty lines)
- Newline at end of string
- Text with newlines and ANSI SGR sequences spanning across line breaks

#### Snapshots

Golden files may shift since tool results and assistant text will render with correct line breaks. Run with `-update` after the fix.

## Files Changed

| File | Change |
|------|--------|
| `internal/agent/events.go` | Add `EndReason` field to `Event` |
| `internal/agent/spawn.go` | Populate `EndReason` on `EventSessionEnded` emit |
| `internal/tui/model.go` | Add `Completed` field to `SessionView` |
| `internal/tui/model_agent.go` | Set `Completed` on session end with reason "done" |
| `internal/tui/model_keys.go` | Gate text input on `Completed`, fix PgUp/PgDn scroll amount |
| `internal/tui/view.go` | Render `[Session complete]` label for completed sessions |
| `internal/tui/ansi.go` | Refactor `WrapText` to handle newlines via `wrapSingleLine` helper |
| `internal/tui/ansi_test.go` | Add newline-related `WrapText` test cases |
| `internal/tui/testdata/*.golden` | Update snapshots if affected |
