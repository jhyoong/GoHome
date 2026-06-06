# Approval Prompt Arrow Key Navigation

## Problem

The tool call approval prompt currently only supports number keys (1-4) for selecting actions. Users expect to be able to navigate with arrow keys and confirm with Enter.

## Approach

Add a `selected int` field to the existing `approvalPrompt` struct (Approach A from brainstorming). This is the smallest change that fits the existing architecture.

## Data model

Add `selected int` to `approvalPrompt` in `approval.go`, initialized to `0` (Allow once) in `newApprovalPrompt`. Values: 0=Allow once, 1=Allow always, 2=Deny, 3=Deny+steer. Only meaningful in top-level menu mode (not editing or steering sub-modes).

## Key handling

In `handleApprovalKey` in `model.go`, add to the top-level approval menu section:

- **Up arrow**: decrement `selected`, clamp to 0
- **Down arrow**: increment `selected`, clamp to 3
- **Enter**: dispatch the action for the currently selected index
- **Number keys 1-4**: still fire immediately as instant shortcuts (unchanged)
- **'e' key**: still enters pattern edit mode (unchanged)

Arrow/Enter handling only applies in top-level menu mode. Editing and steering sub-modes are unaffected.

## Rendering

In `renderApprovalOverlay` in `approval.go`:

- Selected option gets `> ` prefix; others get `  ` prefix (matches timeline cursor convention)
- Number labels `[1]`-`[4]` remain visible
- Footer changes from `Esc: deny` to `Esc: deny | arrows to navigate`
- Highlight marker hidden during editing/steering sub-modes

Example with `selected == 0`:

```
> [1] Allow once
  [2] Allow always   pattern: bash:*  (e to edit)
  [3] Deny
  [4] Deny + steer
Esc: deny | arrows to navigate
```

## Files changed

- `gohome/internal/tui/approval.go` — add `selected` field, update rendering
- `gohome/internal/tui/model.go` — add arrow key + Enter handling in `handleApprovalKey`
- `gohome/internal/tui/tool_test.go` — add tests for arrow navigation
