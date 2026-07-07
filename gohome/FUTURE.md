# Future work

Items explicitly out of v0.2 scope. Each entry notes the seam or hook the v0.2
design preserves so the feature can be added later without a rewrite.

---

**Daemon mode** -- DELIVERED in v0.3.0
The agent runs as a background daemon process communicating with the TUI
over JSON-RPC 2.0 on a Unix socket (`~/.gohome/daemon.sock`). Subagents
are goroutines in the daemon. The TUI is a thin Bubble Tea client that
receives events and sends requests over the same connection. Auto-shutdown
after a configurable grace period when idle.

**Context compaction**
Automatic or manual compaction of long conversation histories to stay within
the context window.
Seam: `session.Session.History` is a plain slice owned by the agent loop.
A compaction step can be injected at the top of `Run` before each `Turn`
call, or as an explicit slash command that rewrites the slice in place.

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

**UI Revamp**
Clean, minimal UI with a focus on readability
Better use of highlights and background
Remove auto-collapse logic

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


## Cleanup backlog

Items from the v0.2 code review (FABLE_REVIEW.md, removed in v0.3.0) that remain
unresolved. All are opportunistic -- safe to fix when touching the surrounding code.

**Dead editor submit/history path** (was M3)
`Model.Update` intercepts Enter and Ctrl+E before `editor.HandleInput`, so
`EditorComponent.Submit`, `SetSubmitHandler`, `onSubmit`, and the input history
are dead code. Fix: route Enter through `editor.Submit()` to restore history,
drop dead handlers, resolve Ctrl+E shadow.

**`retryBackoffMs` config parsed but unused** (was M4)
`Settings.RetryBackoffMs` is loaded and merged but never threaded into `llm.New`.
`config.DefaultRetryBackoff` duplicates `common.DefaultBackoff`. Fix: wire it in
or delete the field.

**Vestigial tool-API surface** (was M5)
`ProgressSink` is always `NullSink{}`, `Result.Details` has no readers, and
`Tool.Execute`'s error return is discarded by `safeExecute`. Fix: wire a real
sink for live bash streaming, or strip the unused parts from the interface.

**Subagent session ID collisions** (was M6, partially resolved)
`session.NewID()` was extracted in v0.3.0, but subagent IDs (`sub-<N>`) still
use a per-process counter that resets on restart. Two runs on the same day can
produce interleaved JSONL. Fix: derive child IDs from the parent or use random IDs.

**Silent error drops in session writer** (was M8, partially resolved)
`guard/check.go` now logs whitelist persist errors (fixed in v0.3.0), but
`session/writer.go` still silently skips unencodable events. Fix: add `slog.Warn`.

**Dead code** (was L1, partially resolved)
`tui/paste.go` + test: only referenced by its own test. `filesearch.go` `cancel`
field is checked but never assigned. Fix: delete paste.go, remove the dead field.

**`style.Theme` mostly bypassed** (was L2)
`chat.go` defines ad-hoc styles that duplicate `style.Theme` fields. Only
`StatusBar` and `Notification` are used from the theme. Fix: consolidate into
`Theme` or delete the unused fields.

**`containsStr` instead of `slices.Contains`** (was L3, partially resolved)
`dirOf` was replaced with `filepath.Dir` in v0.3.0, but `containsStr` in
`guard/persist.go` still reimplements `slices.Contains`.

**Duplicated tool-result merge logic** (was L4)
The "attach result to last pending tool entry" loop exists in both live events
and session replay. Fix: extract a shared helper on `[]TimelineEntry`.

**Timing-based TUI tests** (was L5)
Nine TUI test files use `teatest` with 2-second `WaitFor` polling. The snapshot
suite proves synchronous `Model.Update` works. Convert incrementally when
touching test files.

**Tool reject steering** -- The deny+steer message may be interpreted by the LLM
as tool output rather than a steering instruction. Needs investigation.