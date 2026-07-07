# TUI Polish Design

Targeted improvements to GoHome's TUI user-friendliness, based on comparison with Pi.

## Scope

Four changes, all within `gohome/internal/tui/`:

1. Tool call headers -- contextual short form
2. User message blocks -- full-width background
3. Thinking merge + always visible
4. Expand hint on truncated tool output

## 1. Tool Call Headers

Replace `renderToolLine` with `renderToolSummary` that extracts the key argument per tool type.

| Tool | Display format |
|------|---------------|
| bash | `$ <command>` |
| read | `<file_path>` |
| write | `write <file_path>` |
| edit | `edit <file_path>` |
| subagent | `subagent: <prompt summary>` |
| other | `<toolname>: <first arg summary>` |

Result arrow preserved: `$ ls -la -> 27 lines` or `-> ERROR: msg`.

Status coloring (green=success, yellow=pending, red=error) unchanged. Truncation at max width unchanged.

Key arg extraction: parse `e.Text` as JSON, pull the known field (`command`, `file_path`, `prompt`). Fall back to `shortSummary(e.Text)` on parse failure.

## 2. User Message Blocks

Replace the `you:` prefix with a full-width background block using a left accent border.

Visual:
```
▌ user message text wrapping across
▌ multiple lines
```

- Use lipgloss background (color 236) + left border (colored accent).
- Remove `you:` prefix entirely.
- Background fills to `maxWidth - 2` (accounting for cursor marker).

## 3. Thinking Merge + Always Visible

- On receiving a thinking event, if the last timeline entry is `KindThinking`, append text to it (newline-separated) instead of creating a new entry.
- Remove collapsed rendering. Always render thinking content inline as italic dim text.
- The `Expanded` field on thinking entries is ignored during render (kept for session file compat).

## 4. Expand Hint on Truncated Output

When `e.ToolResult` has more lines than `maxPreviewLines` (currently 3), append a dim hint after the preview:

```
... (N earlier lines, enter to expand)
```

- Same indent as preview lines.
- Applies to both regular and shadow tool entries.
- `N = totalLines - maxPreviewLines`.

## Files Modified

- `chat.go` -- renderEntry, renderToolLine (renamed), KindUser/KindThinking/KindTool branches
- `model_agent.go` -- thinking event merge logic
- `style/style.go` -- possibly add UserBlock style (or define inline in chat.go)
- Snapshot golden files -- regenerated after changes
