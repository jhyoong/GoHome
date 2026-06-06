# Tab Autocomplete for Slash Commands

## Summary

Add Tab-key completion for slash commands in the TUI editor. When the editor content starts with `/` and the user presses Tab, the first matching command is filled in with a trailing space. The slash palette highlights the first match visually.

## Behavior

- Tab pressed while editor starts with `/`: replace editor content with `firstMatch + " "`
- No matches: Tab does nothing (falls through to file-search confirmation)
- Already complete with trailing space (e.g. `/model `): `slashComplete` returns no matches, so Tab does nothing
- File-search Tab confirmation only fires if slash completion did not apply

## Implementation

### 1. New method `completeSlash` on `Model`

- Check if editor value starts with `/`
- Call `slashComplete(editorValue)` to get matches
- If matches exist, set editor value to `matches[0] + " "` and return true
- Otherwise return false

### 2. Update `case tea.KeyTab` in `Model.Update`

Current code only calls `confirmFileSearch()`. Add `completeSlash()` check first:

```go
case tea.KeyTab:
    if m.completeSlash() {
        return m, tea.Batch(cmds...)
    }
    if m.confirmFileSearch() {
        return m, tea.Batch(cmds...)
    }
```

### 3. Update `slashPalette` rendering

Highlight the first match using bold styling. Remaining matches render plain. This signals to the user which command Tab will select.

### Tests

- Tab with `/mo` -> editor becomes `/model `
- Tab with `/` -> editor becomes first match + space (`/new `)
- Tab with `/xyz` -> editor unchanged
- Tab with `/model ` (already complete) -> editor unchanged
- Palette renders first match with bold styling
