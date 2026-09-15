# High-severity fixes design

Date: 2026-09-16
Scope: H1, H2, H3, H4, H5, H6 from the 2026-09-15 code review

## Cluster 1: Steering (H1 + H2)

### H1 — Mid-turn steering corrupts history with >1 tool call

Files: `agent/run.go`

`drainSteer` currently appends the steer user message to `sess.History`
immediately, before tool results are appended. When a steer arrives between
tool calls, history becomes `[assistant(tool_use A, B)] [user(steer)]
[tool(result A)]` — tool_use B has no result, and the user message sits
between assistant and tool_result. Both wire formats reject this.

Fix:

1. `drainSteer` no longer appends to history. It returns the steer text only.
2. When a steer arrives mid-loop, the loop still breaks, but before appending
   the `resultBlocks` message, synthesize error results (`"skipped: user
   steered"`) for every remaining `toolUseBlocks` that were not dispatched.
3. Append the complete `RoleTool` message (all results including synthesized
   ones), then append the steer as a user message.

### H2 — Steer during text-only turn silently delayed

Files: `agent/run.go`, `tui/model.go`

When a turn returns no tool calls, `Run` returns immediately at line 72-74
without draining `steerCh`. The steer stays buffered and resurfaces at an
arbitrary later point. `cancelFocusedSessionWith` clears `pendingMessages`
but not `steerCh`.

Fix:

1. Before returning on the no-tool-calls path, drain `steerCh`. If a message
   is present, append it as the next user message and continue the loop
   instead of returning.
2. In `cancelFocusedSessionWith`, drain and discard `steerCh` on cancel.

## Cluster 2: Compaction and replay (H4 + H5)

### H4 — Auto-compaction sends rejected requests and can cut tool pairs

Files: `agent/compact.go`

Three sub-issues:

1. **Tools: nil** — The summarisation request has `Tools: nil` while
   `oldMessages` contains tool blocks. The Anthropic API rejects this.
   Fix: before building the request, convert all `tool_use` and
   `tool_result` blocks in `oldMessages` to plain text. A helper
   `stripToolBlocks(msgs)` rewrites each tool_use as
   `"[Tool call: <name>(<input>)]"` and each tool_result as
   `"[Result: <text>]"`.

2. **MaxTokens: 0** — `compact()` uses `a.MaxTokens` directly with no
   fallback. After `/model` switches to a config without `maxTokens`,
   this is 0 and the API rejects it. Fix: apply the same fallback as
   `Turn`: `if a.MaxTokens <= 0 { maxTokens = 4096 }`.

3. **Unsafe splits** — The existing guard only checks if `splitIdx` lands
   on a `RoleTool` message. It does not check if `oldMessages` starts
   with a `RoleTool` whose tool_use is in the prefix, or if the prefix
   ends with a tool_use-only assistant message. Fix: extend the guard
   to push `splitIdx` forward if `oldMessages` starts with `RoleTool`,
   and skip the stable prefix if `History[prefixCount-1]` is an assistant
   message containing tool_use blocks with no subsequent tool_result.

### H5 — Resume after compaction rebuilds inconsistent history

Files: `agent/compact.go`, `session/load.go`

The `Compaction` event only persists the summary string. On replay, history
is reset to just the summary, losing `stablePrefix` and `recentMessages`.
Tool results written after the compaction event reference tool_use blocks
that no longer exist.

Fix: change the `Compaction` event to persist a full history snapshot
(`stablePrefix + summary + recentMessages`). On replay, `load.go` replaces
`history` with the snapshot messages instead of just the summary.

## Cluster 3: Standalone (H3 + H6)

### H3 — Headless --prompt - auto-approves without --yolo

Files: `cmd/gohome/main.go`

The `--yolo` check at main.go:210 explicitly excludes `--prompt -`. The
headless frontend returns `AllowOnce` unconditionally, so every tool call
is auto-approved without the user opting in.

Fix: change the condition from `*prompt != "" && *prompt != "-" && !*yolo`
to `*prompt != "" && !*yolo`. Without `--yolo`, `--prompt -` exits with
an error explaining that headless mode requires `--yolo`.

### H6 — The `c` key swallowed when editor is empty

Files: `tui/model_keys.go`

With an empty editor, pressing `c` triggers the copy-to-clipboard action
and returns without inserting the rune. Any message starting with `c`
loses its first character.

Fix: rebind copy from `c` to `Ctrl+Y`. Update the README keybinding table.
