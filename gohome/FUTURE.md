# Future work

Items explicitly out of v0.2 scope. Each entry notes the seam or hook the v0.2
design preserves so the feature can be added later without a rewrite.

---

**Daemon mode**
Run the agent as a background process, with a thin TUI client connecting over
a Unix socket or stdin/stdout JSON-RPC.
Seam: `agent.Frontend` is a pure interface. A second implementation that
serialises all three methods (`Emit`, `RequestApproval`, `AwaitUserInput`)
to JSON-RPC gives daemon mode without touching `internal/agent`,
`internal/llm`, `internal/tools`, `internal/guard`, or `internal/session`.

**Context compaction**
Automatic or manual compaction of long conversation histories to stay within
the context window.
Seam: `session.Session.History` is a plain slice owned by the agent loop.
A compaction step can be injected at the top of `Run` before each `Turn`
call, or as an explicit slash command that rewrites the slice in place.

**Reasoning / thinking tokens**
Surface model chain-of-thought blocks as first-class timeline entries.
Seam: `common.Block` already carries a `Kind` discriminator. A new
`BlockThinking` kind and a corresponding TUI timeline renderer are the only
additions required; the agent loop and adapters need no structural change.

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

**Per-session model selection**
Switch the model mid-session from the TUI without restarting.
Seam: `session.Session.Model` is a plain string field. A `/model` slash
command that updates it and rebuilds the `common.Request` in the next Turn
is the implementation.

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
