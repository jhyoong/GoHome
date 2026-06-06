# Merge /sessions into /resume

## Summary

`/resume` becomes the single command for all session browsing and resuming. `/sessions` is removed entirely. `/resume` always opens the session browser overlay. If an argument is provided (e.g. `/resume login`), the browser's search filter is pre-filled with that text, narrowing the displayed list.

## Motivation

Two separate commands (`/sessions` and `/resume`) serve closely related purposes. Combining them into `/resume` simplifies the command set and makes the most common path (browse then resume) a single command.

## Design decisions

- `/sessions` is removed completely (no alias, no autocomplete entry).
- `/resume` always opens the session browser, even with an argument.
- `/resume <text>` pre-fills the browser's search filter with `<text>`.
- `/resume <id>` no longer resumes directly by ID -- the browser opens with the filter set to `<id>`, which effectively narrows to that session.
- Session delete functionality is preserved in the browser.
- The `--resume` CLI flag is unaffected (separate code path in `main.go`).

## Changes

### 1. `model.go` -- `handleSlashCommand`

- `/resume` case: Replace the current "requires an ID" logic with the session browser body (currently in `/sessions`). If `fields` has a second element, pass it as a pre-fill filter.
- `/sessions` case: Delete entirely.
- `slashCommands` slice: Remove `"/sessions"`.

### 2. `selectlist.go` -- Add `SetQuery` method

Add `SetQuery(q string)` to `SelectListComponent`. Sets `sl.query = q` and calls `sl.applyFilter()`.

### 3. `session_browser.go` -- Add `SetFilter` method

Add `SetFilter(q string)` to `SessionBrowserComponent`. Delegates to `sl.list.SetQuery(q)`.

### 4. `slash.go` -- No changes

`SlashCallbacks` struct stays the same. `ListSessions` and `ResumeSession` are still needed.

### 5. Tests

- Rename `TestSlashSessionsOpensAndCloses` to `TestSlashResumeOpensAndCloses` and change the typed command from `/sessions` to `/resume`.
- Add a test verifying `/resume <text>` pre-fills the filter (browser shows only matching sessions).
- Update any other tests referencing `/sessions` to use `/resume`.

## What stays the same

- `SessionBrowserComponent` behavior (select, cancel, delete).
- `SelectListComponent` filtering, rendering, input handling.
- `SlashCallbacks` struct and wiring in `main.go`.
- `--resume` CLI flag.
- `session.List()` / `session.Listing`.
