# H4 Completion: Active-Modal Key Dispatch

Date: 2026-06-13
Status: Approved
Scope: Key dispatch only (View rendering cascade deferred to a future pass)

## Problem

`handleKeyMsg` in `model_keys.go` is a 6-level priority cascade that checks
boolean flags inline to determine which mode owns the keyboard. Adding a new
overlay or modal requires editing this cascade and getting the ordering right.
The `Interactive` interface exists and five types implement it, but the dispatch
does not use it polymorphically.

## Approach

Add a single `activeModal Interactive` field to `Model`. When non-nil,
`handleKeyMsg` delegates to it. When nil, normal editing mode runs.

This replaces four boolean/pointer field pairs with one polymorphic slot, cutting
the cascade from 6 levels to 3.

## Revised cascade

Before:

    Ctrl+C -> approval -> tokens -> help -> Esc+spinner -> browser -> selector -> normal

After:

    Ctrl+C -> approval -> activeModal -> normal

New `handleKeyMsg`:

```go
func (m *Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    if msg.Type == tea.KeyCtrlC {
        return m.handleCtrlC()
    }
    if m.activeApproval != nil {
        return m, m.handleApprovalKey(msg)
    }
    if m.activeModal != nil {
        cmd := m.activeModal.HandleInput(msg)
        return m, cmd
    }
    return m.handleNormalKey(msg)
}
```

## Components promoted to activeModal

| Component | Already Interactive? | Work needed |
|---|---|---|
| SessionBrowserComponent | Yes | None |
| ModelSelectorComponent | Yes | None |
| SpinnerComponent | Yes | None |
| Tokens overlay | No | New TokensOverlay struct (~30 lines) |
| Help overlay | No | New HelpOverlay struct (~40 lines) |

TokensOverlay and HelpOverlay wrap the existing render/key functions from
`model_overlays.go` into standalone types implementing `Interactive`. Each
takes a close callback (`func()`) that sets `m.activeModal = nil`.

## Fields removed from Model

Removed (6 fields):
- `showTokens bool`
- `showHelp bool`
- `helpScroll int`
- `browsing bool`
- `sessionBrowser *SessionBrowserComponent`
- `selectingModel bool`
- `modelSelector *ModelSelectorComponent`

Added (1 field):
- `activeModal Interactive`

Net: 38 -> 32 fields.

Accessor methods for tests (`ShowTokens()`, `ShowHelp()`, `OpenTokensOverlay()`,
`OpenHelpOverlay()`) are updated to check/set `activeModal` with type assertions.

## Slash command changes

Modal activation in `model_slash.go` changes from setting boolean flags to
assigning the modal component to `activeModal`:

```go
// /resume
m.activeModal = sb

// /model (interactive selector)
m.activeModal = ms

// /tokens
m.activeModal = NewTokensOverlay(sv, m.modelName, m.contextWindow, func() { m.activeModal = nil })

// /help
m.activeModal = NewHelpOverlay(func() { m.activeModal = nil })
```

Each modal's on-cancel/on-select callback sets `m.activeModal = nil`.

## View rendering

Out of scope. The View input-region if/else chain can type-switch on
`m.activeModal` for now, which is no worse than the current flag checks. A
future pass can replace this with `m.activeModal.Render(width)`.

## What stays unchanged

- **Approval**: `activeApproval` / `pendingApprovals` / `handleApprovalKey`
  remain a separate branch. Approval has sub-modes (menu, steer input, pattern
  edit) that don't fit `Interactive` without bloating the interface.
- **File search**: inline with normal editing (intercepts arrow keys inside
  `handleNormalKey`), not a full modal. Stays where it is.
- **Spinner during normal mode**: the Esc-during-spinner check currently runs
  before modal checks. With this design, if the spinner is the `activeModal`,
  it handles Esc via its own `HandleInput`. If the spinner is active but not
  the `activeModal` (running in background while another modal is open), Esc
  goes to the foreground modal, which is correct.

## Testing

- Existing snapshot and teatest-based tests continue to pass (same behavior,
  different dispatch path).
- New: one `Model.Update` test verifying `handleKeyMsg` delegates to
  `activeModal.HandleInput` when set, falls through to `handleNormalKey`
  when nil.

## Daemon mode impact

None. Daemon mode operates at the `agent.Frontend` boundary (agent <-> TUI
transport). This refactoring is entirely within the TUI layer. A daemon TUI
client would reuse the same `Interactive` components.
