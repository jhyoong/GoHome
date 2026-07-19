# Scrolling Enhancements Design

## Problems

1. **Approval prompt blocks timeline scrolling.** When the edit tool shows a large diff in the timeline and an approval prompt is active, Up/Down keys only cycle between approval menu options. The user cannot scroll the timeline to see the full diff. Mouse scroll (translated to arrow keys by the terminal) has the same problem.

2. **Viewport jumps on new content.** After the agent completes tool calls, the viewport jumps to an unexpected position instead of staying where the user was scrolling. This happens because `rebuildViewport()` unconditionally calls `ScrollToBottom()` on every agent event.

## Solution

### 1. Mouse wheel support

Add Bubble Tea mouse support so mouse scroll events are handled separately from arrow keys.

- **Program init** (`main.go:371`): Add `tea.WithMouseCellMotion()` to `tea.NewProgram()` options.
- **Update handler** (`model.go` `Update()`): Add a `tea.MouseMsg` case. `MouseWheelUp` calls `chat.DisableAutoScroll()` + `chat.ScrollUp(3)`. `MouseWheelDown` calls `chat.ScrollDown(3)`. This fires regardless of approval/modal state -- mouse wheel always scrolls the timeline.
- Scroll amount: 3 lines per wheel tick (standard terminal convention).

### 2. PgUp/PgDown during approval

- **Approval key handler** (`model_approval.go` `handleApprovalKey()`): Add `tea.KeyPgUp` and `tea.KeyPgDown` cases in the top-level approval menu section. These scroll the timeline (half-page, same as normal mode) while keeping the approval prompt active.
- Arrow keys (Up/Down) continue to navigate approval menu options.

### 3. Smart auto-scroll

- **rebuildViewport** (`model.go`): Stop unconditionally calling `ScrollToBottom()`. Instead, only call `ScrollToBottom()` if `chat.IsAutoScroll()` is already true. When the user has manually scrolled up (`autoScroll` is false), new content appends but the viewport stays put.
- **User input submission** (`model_keys.go`, Enter path): Explicitly call `chat.ScrollToBottom()` before `rebuildViewport()` so the user sees their own message and the agent response.
- The `keepScroll` parameter on `rebuildViewport()` can be removed since the behavior is now always "preserve current scroll mode."

## Files to modify

| File | Change |
|------|--------|
| `gohome/cmd/gohome/main.go` | Add `tea.WithMouseCellMotion()` |
| `gohome/internal/tui/model.go` | Add `tea.MouseMsg` handler in `Update()`. Change `rebuildViewport()` to preserve scroll state. |
| `gohome/internal/tui/model_approval.go` | Add PgUp/PgDown handling in `handleApprovalKey()`. |
| `gohome/internal/tui/model_keys.go` | Add `chat.ScrollToBottom()` before `rebuildViewport()` on user submit. Remove `keepScroll` args from `rebuildViewport()` calls. |
| `gohome/internal/tui/model_agent.go` | Remove `keepScroll` usage if present. |

## Testing

- Update existing snapshot tests if scroll behavior changes affect golden output.
- Manual testing: verify mouse scroll works in approval mode, PgUp/PgDown scroll during approval, viewport stays put when scrolled up during agent activity, auto-scroll resumes when user scrolls to bottom.
