# Uniform Entry Spacing Design

## Problem

Timeline entries within the "assistant" group (thinking, tool calls, assistant text, stats) run together with no visual breathing room. The current `entryGroup()` function only inserts a blank separator when transitioning between user and assistant groups, leaving everything within a group cramped.

## Approach

Replace the group-based separator logic with a universal rule: insert one blank line before every timeline entry except the first. Delete the `entryGroup()` function entirely.

## Changes

All changes are in `chat.go`.

### 1. Delete `entryGroup()`

Remove the function. It is no longer needed.

### 2. `Render()` loop

Replace the group-transition separator with: before each entry (except the first that produces output), append one blank line to the output slice. Skip the separator for entries that render to zero lines (empty thinking blocks, empty assistant text).

### 3. `countLines()` loop

Same logic as Render: add 1 for each entry after the first that produces lines. Remove the entryGroup check.

### 4. `EnsureCursorVisible()` loop

Same logic: add 1 to cursorTop for each entry after the first that produces lines. Remove the entryGroup check.

## What stays the same

- All entry rendering (renderEntry) is untouched.
- Scroll logic, cache logic, scrollbar logic unchanged.
- Visual style of each entry type (tool blocks, user blocks) unchanged.

## Testing

- Regenerate all golden snapshot files with `-update`.
- Verify snapshot tests pass after regeneration.
- Manual verification that spacing looks correct.
