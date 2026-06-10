# FABLE_REVIEW: gohome codebase review

Date: 2026-06-10
Scope: full source tree under `gohome/` (~19.4k LOC including tests; ~8.6k non-test).
Method: an initial subagent review followed by manual verification of every claim
against the source. Each finding below was re-checked by reading the cited code.
Two claims from the initial review were corrected (see "Corrections" at the end).

Baseline at time of review:
- `go vet ./gohome/...` clean
- `go test ./gohome/...` all packages pass
- `cmd/gohome` has no test files

Priorities reflect maintainability impact for future expansion, per the project
goal of keeping the codebase clean, small, and easy to extend.

---

## High priority

### H1. Turn cancellation is not wired end-to-end — VERIFIED

`/cancel`, Esc-during-spinner, and single Ctrl+C all route to
`cancelFocusedSessionWith` (`gohome/internal/tui/model.go:434-447`), which calls
`m.slashCB.CancelSession` only if non-nil. The `SlashCallbacks` struct declares
the field (`gohome/internal/tui/slash.go:13`), but `main.go` never sets it — the
callbacks block at `gohome/cmd/gohome/main.go:307-373` wires `ListSessions`,
`ResumeSession`, `NewSession`, and `SetModel` only.

Result: the TUI sets `InFlight = false`, appends a "Cancelled." notice, and
stops the spinner, while the agent goroutine keeps streaming. The next
`EventTokenDelta` sets `InFlight = true` again (`model.go:306`), so the UI
visibly contradicts itself. The agent layer fully supports per-turn
cancellation — `Run` handles `ctx.Err()` cleanly (`gohome/internal/agent/run.go:29-38`)
and `agent/cancel_test.go` proves it. Only the wiring is missing: the sole
context is the whole-process one created at `main.go:382`.

Why it matters: cancellation is a core control path. Any future feature built
on it (retries, parallel sessions, turn timeouts) inherits this silent
UI/agent divergence.

Suggested fix (KISS): in the agent driver loop in `main.go`, wrap each
`a.Run` call in `context.WithCancel(ctx)`, store the cancel func where the
callbacks can reach it, and set `CancelSession: func(string) { cancelTurn() }`.

### H2. Data races and a send-on-closed-channel panic path in session-swap callbacks — VERIFIED

The slash callbacks execute on the Bubble Tea goroutine (they are called
synchronously from `handleSlashCommand`, `model.go:1056-1101`). `ResumeSession`
(`main.go:311-342`) and `NewSession` (`main.go:343-368`) reassign the captured
`sess` and `writer` variables and mutate `a.Session` / `a.Writer`; `SetModel`
(`main.go:369-372`) writes `sess.Model`. Concurrently, the agent goroutine
(`main.go:395-421`) reads `sess.ID`, appends to `sess.History`, calls
`writer.Emit`, and runs `a.Run(ctx, sess)`.

Nothing prevents `/new` or `/resume` while a turn is in flight. Two concrete
failure modes:

1. Plain data race on `sess`, `writer`, `a.Session`, `a.Writer` (unsynchronized
   write from the TUI goroutine, read from the agent goroutine).
2. Panic: both callbacks call `writer.Close()` (`main.go:331,361`), which
   closes the writer channel (`gohome/internal/session/writer.go:46-48`).
   `Writer.Emit` is a bare channel send (`writer.go:39-41`), so a concurrent
   `Emit` from the agent or its in-flight tool results is a send on a closed
   channel.

Why it matters: this is the most likely crash/corruption source in the
codebase, and it sits in the only untested package (`cmd/gohome`).

Suggested fix (KISS): own `sess` + `writer` in a small mutex-guarded struct
(in `main.go` or `internal/agent`), and make the swap callbacks refuse — or,
once H1 is fixed, first cancel — when a turn is in flight. Add a regression
test for "switch session mid-turn" and run it under `-race`.

### H3. OpenAI adapter silently drops parallel tool results — VERIFIED

`Agent.Run` batches all tool results of a turn into a single `RoleTool`
message (`gohome/internal/agent/run.go:94-98`). On the OpenAI wire,
`translateToolMessage` (`gohome/internal/llm/openai/request.go:193-211`)
serializes only `m.Content[0]`. The comment at lines 200-201 says the code
should "error on multi", but it neither errors nor handles additional blocks —
they are silently dropped.

Consequence: whenever the model issues two or more tool calls in one assistant
turn against an OpenAI-format endpoint, all results after the first vanish.
OpenAI rejects conversations with unanswered `tool_call_id`s, or the model
mis-reasons from missing results. The Anthropic adapter is unaffected (it
carries all blocks in one user message).

Suggested fix (KISS): in `buildOpenAIBody` (`request.go:92-98`), flatten
`RoleTool` messages into one `openaiToolMessage` per `BlockToolResult` block
(the builder already owns the `msgs []any` slice). Add a translate test with
two results in one message.

### H4. `tui.Model` is a god object with a modal mega-Update — VERIFIED

`gohome/internal/tui/model.go` is 1407 lines; the next-largest production file
is `main.go` at 449. `Model` has roughly 30 fields spanning every concern
(sessions, editor, chat, spinner, file search, browser, model selector,
approvals, overlays, context warnings, slash callbacks). `Update`
(`model.go:570-830`) is a hand-rolled mode router: Ctrl+C double-tap, approval
keys, tokens overlay, help overlay, browser, model selector, Esc/spinner,
file search, timeline cursor, and finally the editor — each as an early-return
ladder whose ordering is load-bearing and subtle (e.g. the Esc special case at
`model.go:660-664` must run after the browser/selector checks would not, so it
explicitly excludes them with `!m.browsing && !m.selectingModel`).
`handleSlashCommand` (`model.go:1027-1160`) constructs entire sub-UIs inline.

Why it matters: every new overlay or mode adds another branch to one function
and another field to one struct, and each addition can silently change which
mode wins a key.

Suggested fix (KISS): no framework needed. Split by mode into files
(`model_keys.go`, `slash_commands.go`, `overlays.go`) and reduce `Update` to
"resolve active mode, delegate to its handler". The `Interactive` interface
(`gohome/internal/tui/component.go:13-16`) already defines exactly this
contract and is barely used.

---

## Medium priority

### M1. The Anthropic and OpenAI clients duplicate the HTTP/stream scaffold — VERIFIED

`gohome/internal/llm/anthropic/client.go` and
`gohome/internal/llm/openai/client.go` are structurally identical (verified by
diff): same struct shape, same `New`, same retry loop, same non-2xx handling,
same SSE pump goroutine. The only real differences are the URL path, two
header lines, the body-builder call, and error-message prefixes. The two
`retry.go` files are identical except for the package clause and one comment;
both just alias `common.DefaultBackoff`.

Why it matters: a third wire format currently costs ~350 lines of copied
scaffold, and a bug fix in the retry/pump logic must be applied twice.

Suggested fix (KISS): add a shared helper in `llm/common`, e.g.
`StreamRequest(ctx, httpClient, backoff, buildReq func() (*http.Request, error)) (io.ReadCloser, error)`
plus a shared event-pump. Each adapter keeps only body building, headers/URL,
and translation (legitimately wire-specific). Delete both `retry.go` files.

### M2. `/endpoint` and `/model` are half-implemented — VERIFIED

- `/endpoint` is listed in `slashCommands` (`model.go:1011`) and documented in
  the help overlay (`gohome/internal/tui/help.go:26`), but `handleSlashCommand`
  has no case for it — it autocompletes and then falls through to
  `": unknown command"` (`model.go:1157`).
- The `/model` endpoint selector (`model.go:1136-1155`) sets
  `m.settings.DefaultEndpoint` and calls `SetModel`, but the `SetModel`
  callback (`main.go:369-372`) only does `sess.Model = name`. The LLM client —
  wire format, base URL, API key — is built once at startup (`main.go:167`)
  and never rebuilt. Selecting a model from a different endpoint sends the new
  model name to the old endpoint.

Suggested fix: either implement the client swap in the callback (`llm.New`
already exists as the factory) or remove `/endpoint` from `slashCommands` and
help until it works. A menu that silently does the wrong thing is worse than
no menu. Note that a client swap must also respect H2 (no swapping mid-turn).

### M3. Dead editor submit/history path plus a Ctrl+E key conflict — VERIFIED

`Model.Update` intercepts Enter (`model.go:696-744`) and Ctrl+E
(`model.go:745-746`) before `editor.HandleInput` is reached. Consequences:

- `EditorComponent.Submit` (`gohome/internal/tui/editor.go:182-190`),
  `SetSubmitHandler`/`onSubmit` (`editor.go:44-47`), and the editor's own
  Enter handling (`editor.go:292-302`) never execute in production.
- `history.Add` is only called inside `Submit`, so the input history is never
  populated. The history-browsing UI (Up-arrow at line 0,
  `editor.go:308-316`; `history.go`, 67 lines plus tests) is reachable but
  always browses an empty history — a dead feature that looks alive.
- The editor's Ctrl+E (end-of-line, `editor.go:371`) is shadowed by the
  external-editor binding in `Update`.

Suggested fix: have the Model's Enter branch call `editor.Submit()` (which
restores history recording), delete `SetSubmitHandler`/`onSubmit`, and pick a
non-conflicting key for the external editor or drop the editor-internal
Ctrl+E binding deliberately.

### M4. `retryBackoffMs` config option is parsed but never used — VERIFIED

`Settings.RetryBackoffMs` is declared, loaded, and merged
(`gohome/internal/config/config.go:44,79,110-112`) but consumed nowhere.
`config.DefaultRetryBackoff` (`config/defaults.go:15`) duplicates
`common.DefaultBackoff` (`gohome/internal/llm/common/retry.go:12`) value for
value; both clients hardcode the latter.

Why it matters: a documented setting that does nothing is a user trap and a
maintainer trap.

Suggested fix: thread `Settings.RetryBackoffMs` into `llm.New`, or delete the
field and `config.DefaultRetryBackoff`. Either is fine; pick one.

### M5. Vestigial tool-API surface: ProgressSink, Result.Details, ignored error — VERIFIED

- The only `ProgressSink` ever passed is `tools.NullSink{}`
  (`gohome/internal/agent/run.go:146`). The bash tool carefully fans each
  output line into the sink (`gohome/internal/tools/bash.go:102-114`) for no
  consumer.
- `Result.Details` (`gohome/internal/tools/tool.go:22`) has zero readers.
- `Tool.Execute`'s error return is discarded by the dispatcher
  (`run.go:146`, `res, _ := safeExecute(...)`).

Why it matters: every new tool author has to satisfy (and wonder about) three
pieces of interface that do nothing.

Suggested fix (pick one): wire a real sink to `Frontend.Emit` to stream bash
output live, or strip `sink`, `Details`, and possibly the error return from
the interface. A smaller `tools.Tool` makes new tools easier to write
correctly.

### M6. Subagent session files can collide across runs — VERIFIED

Child IDs are `sub-<N>` from a per-process atomic counter
(`gohome/internal/agent/agent.go:33-39`, `gohome/internal/agent/spawn.go:29`),
restarting at 1 every process. `SessionPath` is
`<home>/sessions/<slug>/<date>-<id>.jsonl`
(`gohome/internal/session/paths.go:20-24`), and `OpenWriter` opens with
`O_APPEND` (`writer.go:24`). Two gohome runs in the same project on the same
day both append their first subagent to `<date>-sub-1.jsonl`, producing one
file with two `session_start` events and interleaved histories.

Suggested fix: derive child IDs from the parent (`<parentID>-sub-N`) or reuse
the random `newSessionID()` — which would mean moving it from `main.go:40-47`
into the `session` package, where it arguably belongs anyway.

### M7. `agent.Frontend` is broader than the agent's needs; input routing ignores sessions — VERIFIED

`agent.Frontend` (`gohome/internal/agent/events.go:52-64`) declares `Emit`,
`RequestApproval`, and `AwaitUserInput`. Verified by grep: the agent package
itself only calls `Emit`. `RequestApproval` is called only by the guard, which
already defines its own single-method `Frontend` interface
(`gohome/internal/guard/check.go:11-13`). `AwaitUserInput` is called only by
`main.go:398`.

Additionally, `AwaitUserInput` ignores its `sessionID` parameter
(`gohome/internal/tui/frontend.go:94`, `_ string`), and the input channel is
global. Typing while focused on a subagent session appends the text to the
focused (subagent) timeline (`model.go:722-741`) but delivers it to the main
agent's REPL loop — the displayed conversation and the actual history disagree.

Suggested fix (KISS): shrink `agent.Frontend` to `Emit`; keep `guard.Frontend`
as is; define the REPL-input contract where it is consumed (`main.go`). Then
either honor `sessionID` in input routing or block text input while a
non-main session is focused.

### M8. Two quiet failure paths swallow errors without logging — VERIFIED

- `gohome/internal/guard/check.go:66-70`: when `AllowAlways` persistence
  fails, the error is discarded with `_ = err`. The comment says "Log but
  don't fail" — there is no log. The user believes a whitelist entry was
  saved; it may not have been, and they will be re-prompted with no
  explanation.
- `gohome/internal/session/writer.go:66-69`: unencodable events are dropped
  silently in the writer goroutine.

The rest of the codebase consistently uses `slog.Warn` for soft failures
(`config.go:54`, `load_whitelist.go:30`, `compile.go:50`). Bring these two in
line.

---

## Low priority

### L1. Dead code — VERIFIED

- `gohome/internal/tui/paste.go`: `PasteMsg`, `EnableBracketedPaste`,
  `DisableBracketedPaste`, `IsPasteMsg` are referenced only by
  `paste_test.go`. Production paste handling goes through `msg.Paste` in
  `editor.go:286-289`. Delete the file and its test.
- `gohome/internal/tui/filesearch.go:43`: the `cancel context.CancelFunc`
  field is checked in `Hide()` (lines 88-90) but never assigned anywhere
  (the `cancel` at line 131 is a local in a closure).
- `gohome/internal/agent/events.go:15,21`: `EventToolCallStart` is never
  emitted by anyone. `EventSessionEnded` is never emitted in production code
  (the TUI handles it at `model.go:382,411`, but subagent completion is
  signalled via `EventTurnDone`). Either emit them or remove them.
- `gohome/internal/tui/frontend.go:27-29,37-38`: `agentEventMsg` and
  `externalEditorMsg` are aliases kept only so old names compile. Rename the
  uses and delete the aliases.

### L2. `style.Theme` is mostly bypassed — VERIFIED

`gohome/internal/tui/style/style.go` defines `UserMsg`, `ToolPending`,
`ToolSuccess`, `ToolError`, etc., but `chat.go:10-14` defines its own
`userPrefix`/`noticeStyle`/`expandedBg` package vars and `renderToolLine`
(`chat.go:247-255`) rebuilds the three tool styles inline with the same color
codes. Only `StatusBar` and `Notification` are actually used from the theme.
Fold the ad-hoc styles into `Theme` so theming is a single-file change, or
delete the unused theme fields.

### L3. Hand-rolled helpers where stdlib exists — VERIFIED

`gohome/internal/guard/persist.go:116-133`: `containsStr` is
`slices.Contains` (stdlib since Go 1.21; project is on 1.25), and `dirOf`
reimplements `filepath.Dir`. Minor behavioral note for the swap: `dirOf`
returns `""` when there is no separator while `filepath.Dir` returns `"."` —
check the call site when replacing.

### L4. Duplicated tool-result merge logic — VERIFIED

The "attach result to the last pending tool entry" loop exists twice:
`model.go:325-356` (live events) and
`gohome/internal/tui/history_convert.go:60-76` (session replay). One shared
helper on `[]TimelineEntry` would keep the two paths from drifting.

### L5. Timing-based TUI test pattern is widespread — VERIFIED, CORRECTED FROM INITIAL REVIEW

The initial review claimed teatest appears only in `strip_test.go` and could
be dropped from `go.mod` by converting one file. That is wrong: teatest with
2-second `WaitFor` polling is used across nine TUI test files
(`tui_test.go`, `approval_test.go`, `slash_test.go`, `tool_test.go`,
`statusbar_test.go`, `help_test.go`, `integration_test.go`, `tokens_test.go`,
`context_warn_test.go`, `strip_test.go`). The snapshot suite
(`tui_snapshot_test.go`) is fully synchronous, and CLAUDE.md's "no goroutines
or teatest" statement is scoped to the snapshot tests, so there is no doc
drift either.

What remains true: the polling pattern is slower and more timing-sensitive
than the synchronous `Model.Update` pattern the snapshot tests prove works.
Converting these files is a worthwhile but larger cleanup than first reported,
and only after that would teatest be removable from `go.mod`. Reasonable to do
incrementally — convert files when you touch them.

### L6. Test coverage gaps — VERIFIED

- `gohome/cmd/gohome` has no test files (confirmed by `go test` output). The
  session-swap closures from H2 and the resume/new writer lifecycle are the
  riskiest untested code in the repo. Extracting them from `main()` into a
  testable type would address H2 and this gap together.
- No test exercises concurrent `Writer.Emit` + `Writer.Close`
  (`session/writer.go`) under `-race`, which is exactly the H2 panic path.

---

## What's already good

- Package boundaries are clean and enforced: `agent` never imports `tui`;
  `guard` defines its own narrow `Frontend`; compile-time interface
  assertions exist at `tui/frontend.go:15-18`.
- Test breadth is strong: 79 test files covering guard matching, config
  merging, session load/replay, SSE parsing and message translation for both
  wire formats, and golden-snapshot TUI rendering. Everything passes, vet is
  clean.
- `go.mod` is tight. Every direct dependency is genuinely used (chroma and
  goldmark for markdown, uniseg for width, clipboard, bubbles for the
  approval text inputs, the charm stack). teatest is test-only.
- The guard design (whitelist compile, pattern suggestion, persistence with
  file locking), the JSONL writer's fsync-on-critical-events policy,
  `safeExecute` panic recovery, and the documented shutdown ordering in
  `main.go:375-381` are all thoughtful, well-engineered pieces.
- `SelectListComponent` is properly reused by the session browser and model
  selector. The same consolidation instinct applied to the LLM clients (M1)
  and the Model decomposition (H4) is the main growth area.

## Corrections made during verification

1. L5 (was L4): the teatest claim was materially wrong — teatest is used in
   nine test files, not one, and there is no CLAUDE.md doc drift. Reframed
   above.
2. M1: the two `retry.go` files were described as "character-for-character"
   identical; they differ by the package clause and one comment line. The
   substance of the finding stands.

All other findings were confirmed as originally reported, with line numbers
re-checked against the current source.

## Suggested sequencing

1. H1 + H2 together: per-turn cancellation, then a mutex-guarded session/writer
   owner that refuses swaps mid-turn. These fix the only crash/correctness
   bugs in the core loop. Add the `-race` test from L6.
2. H3: small, isolated, testable wire-format fix.
3. M2, M3, M8: small correctness/consistency fixes, each under an hour of
   review surface, no design decisions needed.
4. H4 and M1: the structural refactors that most directly serve the goal of
   cheap future expansion (new TUI modes, new LLM providers).
5. M4-M7, L1-L4: opportunistic cleanups; safe to batch or do when touching
   the surrounding code.
