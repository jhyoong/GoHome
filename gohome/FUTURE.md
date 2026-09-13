# Future work

Items explicitly out of v0.2 scope. Each entry notes the seam or hook the v0.2
design preserves so the feature can be added later without a rewrite.

---

**Daemon mode** -- TRIED in v0.3.0, REVERTED in v0.4.0
Implemented as a background daemon with JSON-RPC over Unix socket in v0.3.0.
Reverted in v0.4.0 due to 8+ critical bugs (concurrency races, channel stalls,
goroutine leaks, Ctrl+C hangs), broken Windows support, and unnecessary complexity
for a single-user CLI tool. The in-process architecture was restored with all
non-daemon features ported forward.

**Context compaction** -- DELIVERED in v0.4.1
Auto-compaction summarizes conversation history when token usage approaches
the context window limit. Configurable via percentage or leftover trigger modes.
A `Compaction` event is persisted to the session file for resumption.

**Reasoning / thinking tokens** -- DELIVERED in v0.2.1
Thinking blocks are parsed from Anthropic SSE streams and OpenAI `reasoning_content`
deltas, rendered as collapsible timeline entries with line counts, persisted to
session JSONL, and restored on resume.

**Denylist**
Complement to the whitelist — keep specific patterns blocked even if they
would otherwise match an allow rule.
Seam: `guard.Whitelist` and `guard.Compile` are the only callsites. A
`DenyPatterns` field in `WhitelistFile` and a pre-allow check in
`Whitelist.Allows` is the full implementation surface.

**In-flight subagent steering**
Inject a user message into a running subagent's conversation without
cancelling it.
Seam: `agent.Frontend.AwaitUserInput` is already wired; in the subagent
loop the parent could push a message into the child's `sess.History` via a
channel exposed on `session.Session`, then unblock the child's turn.

**Cross-session search / history browsing UI**
Search across all past JSONL sessions for a project.
Seam: `session.List` and `session.Load` already provide the reading
primitives. A search TUI view is purely additive; no agent or session changes
are needed.

**Markdown export**
Export a session transcript as a Markdown file.
Seam: `session.Load` reconstructs the full event stream. An export function
that walks `sess.History` and formats it is fully self-contained.

**Custom themes**
User-selectable colour schemes beyond the default terminal-aware theme.
Seam: `internal/tui/style` isolates all Lip Gloss styles in one struct. A
theme loader that builds that struct from a JSON/TOML file is the only
change needed.

**Per-subagent independent whitelist**
Allow each subagent to operate with a stricter or different whitelist than
the parent.
Seam: `guard.Guard` is injected into the agent at construction. `agent.Spawn`
already creates a child `Agent`; passing a different `Guard` instance
(compiled from a per-subagent whitelist file) is a one-line change in Spawn.

**Per-session model selection** -- DELIVERED in v0.2.1, COMPLETED in v0.2.4
`/model <name>` switches the active model and rebuilds the LLM client at runtime.
Without arguments, an interactive model selector shows all configured model configs.
Cross-endpoint model selection now fully works (M2 resolved in v0.2.4).

**Mouse support**
Click to focus sessions, scroll with the mouse wheel.
Seam: Bubble Tea supports mouse events natively. Enabling
`tea.WithMouseCellMotion()` and adding mouse message handlers in
`tui.Update` is the full scope.

**Image rendering**
Display image outputs from tools (sixel / kitty graphics protocol).
Seam: `common.Block` can carry a new `BlockImage` kind. The TUI timeline
renderer dispatches on kind; the agent and adapters are unaffected.

**Audit log**
Write every approval decision (with timestamp, session ID, tool name, outcome)
to `~/.gohome/audit.log` for compliance or review.
Seam: `session.Approval` events are already emitted by the agent. A parallel
writer in `guard.Guard` or `agent.dispatchTool` can tee decisions to a
separate append-only file without touching any other package.

**SSE-parser fuzzing, benchmark suite, coverage gates**
Automated quality gates beyond the current unit-test suite.
Seam: SSE parsing is isolated in `internal/llm/anthropic/sse.go` and
`internal/llm/openai/sse.go`. Both are pure functions over byte slices,
making them natural fuzzing targets. Benchmark entry points can be added to
existing `_test.go` files with no structural changes.


## Additional findings
- ~~Tool calls to show last 3 rows of output~~ -- DELIVERED in v0.2.5
- ~~Edit tool to show the changes made (git diff style)~~ -- DELIVERED in v0.2.5
- ~~Scrolling doesn't work properly when in edit tool mode~~ -- Scrolling reworked in v0.4.1 (smart auto-scroll, mouse wheel, PgUp/PgDn during approval). Sudo cases now have dedicated approval prompts.
- Steering or adding in prompts mid turn ( inbetween tool calls ) seems to not work as intended, and only send after the agent finishes all of its tool calls?
- After the recent mouse scrolling update, selecting lines for copy pasting doesn't work anymore.
- Multiple tool calls - wrong output displayed. E.g tool call 1 shows results of tool call 2, and vice versa.
- Drastic slowdown on rendering when large thinking or replies after 40k context?
- Auto-compact doesn't seem to work properly- the notice triggers, but the conversation history sent to the LLM endpoint seems to invalidate the entire cache? Is it because it's modified from the system prompt? Should it be tied to a 'user message` but sent by the system instead?
- When LLM intends to run a inline python tool call - it can grow to really long tool approval prompt. Cant scroll properly to view the entire thing.
- (Windows) When pasting multiple lines ( e.g copied from notepad, multiple commands ) - it seems to send the first line only ( might be due to breaklines or something? ) instead of the whole chunk. While default terminal behaves that way, gohome is a coding assistant so pasting long chunks of text should be the norm, and send only when pressing enter is the behaviour to match.
  - Related point: When multiple messages are then queued - they fire off in turns and not altogether as one message. this causes issues. Might need a way to handle/edit/delete queued messages.