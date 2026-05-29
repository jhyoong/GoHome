# GoHome — Design

**Status:** Approved 2026-05-29. Ready for implementation planning.
**Working name:** `gohome` (placeholder; rename freely).
**Scope:** A Go variant of `pi`, limited to the coding agent and TUI.

## Purpose & constraints

GoHome is a lightweight, single-binary coding agent and TUI written in Go. It targets custom LLM endpoints — both OpenAI-compatible (chat completions) and Anthropic-styled (messages) wire formats — and centers two features that pi leaves to extensions:

1. **Tool-call guardrails** with human approval, a skip mode, and a whitelist.
2. **Built-in subagents** the user can focus into, approve tool calls for, and steer.

Hard constraints:

- Lightweight is a primary goal, not aspirational. Small binary, few deps, narrow scope.
- The target environment has limited token-processing throughput. The agent is paired with slower endpoints, so concurrency does not help throughput — this is why subagents are sequential, not parallel.
- No multi-provider abstraction. Two thin wire-format adapters and that is the whole LLM layer.

What is explicitly cut from v1 (parked under future work, listed at the end):

- Context compaction. Reasoning/thinking tokens as first-class blocks. Denylist. In-flight subagent steering. Cross-session search. Markdown export. Custom themes. Per-subagent independent whitelist. Per-session model. Mouse support. Image rendering.

---

## 1. Architecture & package layout

One Bubble Tea process. Each agent session — parent or subagent — runs in its own goroutine and talks to the TUI through a small `Frontend` interface, with channels under the hood.

### Package layout

```
gohome/
  cmd/gohome/              # main, flag parsing
  internal/
    agent/                 # turn loop, message types, Frontend interface
    llm/
      openai/              # OpenAI-compatible (chat completions) adapter
      anthropic/           # Anthropic-styled (messages) adapter
      common/              # Message, Block, StreamEvent, Usage
    tools/                 # read, write, edit, bash, subagent
    guard/                 # whitelist matching, approval engine, yolo
    session/               # in-memory session + JSONL persistence
    config/                # settings.json + whitelist.json loaders
    tui/
      views/               # chat, approval, focus strip, status bar
      style/               # Lip Gloss styles
  docs/
```

### The seam — `Frontend` interface

The agent depends only on this interface. Nothing in `internal/agent` imports `tui`.

```go
type Frontend interface {
    Emit(sessionID string, ev Event)
    RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalDecision, error)
    AwaitUserInput(ctx context.Context, sessionID string) (string, error)
}
```

### Concurrency model

- One root `context.Context` per session, cancellable from the TUI.
- Each session goroutine owns its own message history and tool execution.
- Subagent goroutine is spawned by the parent's `subagent` tool, blocks the parent until done, returns a single text result.
- `Frontend` calls are safe from any goroutine; the TUI funnels everything onto Bubble Tea's `Update` thread via `tea.Msg`.

### Daemon upgrade path (not built in v1; documented so the seam stays clean)

Because `internal/agent` only depends on `Frontend`, a future `--daemon` mode can ship a second implementation of `Frontend` that serializes calls to JSON-RPC over stdin/stdout (or a Unix socket). A thin TUI client on the other end renders the screen and answers approval requests across the wire. No changes needed to `internal/agent`, `internal/llm`, `internal/tools`, `internal/guard`, or `internal/session`. Concretely, the daemon work becomes:

- Define the JSON-RPC method set (mirrors `Frontend` 1:1).
- Add `internal/rpc/server` (host-side `Frontend` adapter).
- Add `internal/rpc/client` (client-side `Frontend` over a pipe).
- Add `--daemon` and `--connect <socket>` flags to `cmd/gohome`.

This grows GoHome into the daemon model without a rewrite. Any change in v1 that breaches the seam (e.g. the agent reaching into TUI state directly) should be treated as a regression.

---

## 2. LLM adapters

Two thin adapters behind a common `Client` interface. Both stream via SSE. Both normalize to one internal message and tool-call shape.

### Common interface

```go
type Client interface {
    Stream(ctx context.Context, req Request) (<-chan StreamEvent, error)
}

type Request struct {
    Model     string
    System    string
    Messages  []Message
    Tools     []ToolDef
    MaxTokens int
}

type Message struct {
    Role    Role     // user | assistant | tool
    Content []Block  // text / tool_use / tool_result
}

type StreamEvent struct {
    Kind        EventKind  // delta | tool_call_partial | tool_call_done | turn_done | err
    TextDelta   string
    ToolCallID  string
    ToolName    string
    InputJSON   string     // accumulated by adapter; emitted on tool_call_done
    StopReason  string     // on turn_done: end_turn | tool_use | max_tokens
    Usage       *Usage     // populated on turn_done; nil otherwise
    Err         error
}
```

### Adapter responsibilities

Each adapter is a pure translation layer (<500 LOC):

1. Convert the common `Request` into the wire format.
2. Open an SSE stream.
3. Translate wire-format events back into the common `StreamEvent` stream.

Tool-call shape differences (handled inside the adapters, invisible to the agent):

- **Anthropic** streams `tool_use` blocks with incremental `input_json_delta` events. The adapter accumulates the JSON and emits one `tool_call_done` when the block finishes.
- **OpenAI** streams `tool_calls[].function.arguments` deltas. Same accumulation, same emit.

### Endpoint configuration

`~/.gohome/settings.json` (global) and `./.gohome/settings.json` (project), merged at load.

```json
{
  "endpoints": {
    "local-anthropic": {
      "wire": "anthropic",
      "baseURL": "http://localhost:8080",
      "apiKeyEnv": "GOHOME_API_KEY",
      "defaultModel": "claude-opus-4-7",
      "contextWindow": 200000,
      "headers": { "X-Custom": "value" }
    },
    "local-openai": {
      "wire": "openai",
      "baseURL": "http://localhost:8081/v1",
      "apiKeyEnv": "GOHOME_API_KEY",
      "defaultModel": "gpt-4o",
      "contextWindow": 128000
    }
  },
  "defaultEndpoint": "local-anthropic"
}
```

CLI overrides: `--endpoint <name>`, `--model <name>`.

---

## 3. Tool system

Five tools implementing a small interface; tools are pure execution units. Guardrails and TUI rendering wrap them from the outside.

### Tool interface

```go
type Tool interface {
    Name() string
    Description() string
    InputSchema() json.RawMessage   // JSON Schema
    Execute(ctx context.Context, in json.RawMessage, sink ProgressSink) (Result, error)
}

type ProgressSink interface { Update(chunk string) }

type Result struct {
    Content string  // tool_result text returned to the LLM
    IsError bool
    Details any     // structured metadata for TUI rendering
}
```

### The five tools

| Tool | Input | Behavior |
|---|---|---|
| `read` | `{path, offset?, limit?}` | Returns file contents with 1-indexed line numbers (`cat -n` style). Default limit 2000 lines. Adds file to per-session "files read" set. |
| `write` | `{path, content}` | Creates or overwrites a file. Requires parent dir to exist. |
| `edit` | `{path, old_string, new_string, replace_all?}` | Exact string replacement. Errors if `old_string` is not unique (unless `replace_all`). Requires the file to have been `read` earlier in the session. |
| `bash` | `{command, timeout_ms?, cwd?}` | Runs via `/bin/sh -c` (Unix) or `cmd /c` (Windows). Streams stdout+stderr through `ProgressSink`. Default timeout 120s, max 600s. |
| `subagent` | `{task, system_prompt?}` | Spawns a fresh isolated subagent. See Section 5. |

### Working directory and session state

- Each session has a `cwd` (defaults to launch directory). File-path tools resolve relative paths against it.
- `read` mutates the per-session "files read" set used by `edit`'s sanity check.
- `bash`'s `cwd` parameter overrides the session `cwd` for that one call without mutating session state.

### Registration

```go
type Registry struct { ... }
func (r *Registry) Register(t Tool)
func (r *Registry) Get(name string) (Tool, bool)
func (r *Registry) Schemas() []ToolDef
```

### Error handling inside tools

- A returned `error` means wire failure (disk full, OS permission denied). TUI renders a red banner.
- A returned `Result{IsError: true, Content: "..."}` means an expected error the LLM should see and react to. TUI renders an inline error block. Both forms become a `tool_result` block back to the LLM.

---

## 4. Guardrail engine

A small `Guard` component that, before any tool call, consults the whitelist (or yolo mode) and asks the user via `Frontend.RequestApproval` when no rule covers the call. The user's "Allow always" choice writes back to the project whitelist file.

### Whitelist files

Two files, merged at load. Project entries are additive on top of global; a project cannot remove a global allow.

- `~/.gohome/whitelist.json` (global)
- `./.gohome/whitelist.json` (project; new "Allow always" entries land here by default)

```json
{
  "tools": ["read"],
  "bash": [
    "^git status",
    "^git log",
    "^ls",
    "^echo "
  ]
}
```

- `tools` — tool names auto-approved.
- `bash` — regex patterns checked against the bash command string. Patterns are auto-anchored at the start (`^` prepended if missing).

### Matching logic

```go
type Guard interface {
    Check(ctx context.Context, sessionID, tool string, input json.RawMessage) (Decision, error)
}

// Pseudocode:
func (g *guard) Check(...) (Decision, error) {
    if g.yolo.Load() { return Decision{Allow: true, Reason: "yolo"}, nil }
    if g.whitelist.Allows(tool, input) { return Decision{Allow: true, Reason: "whitelisted"}, nil }

    req := ApprovalRequest{
        Tool: tool, Input: input,
        Summary: g.summarize(tool, input),
        SuggestedPattern: g.suggestPattern(tool, input),
    }
    dec, err := g.frontend.RequestApproval(ctx, req)
    if err != nil { return Decision{}, err }

    switch dec.Outcome {
    case AllowOnce:   return Decision{Allow: true, Reason: "user_once"}, nil
    case AllowAlways: g.whitelist.AddProject(tool, dec.SavedPattern)
                      return Decision{Allow: true, Reason: "user_always"}, nil
    case Deny:        return Decision{Allow: false, Reason: "user_denied"}, nil
    case DenySteer:   return Decision{Allow: false, Reason: "user_denied_steer",
                                      SteerMessage: dec.SteerMessage}, nil
    }
}
```

### Pattern suggestion heuristic

When the user picks "Allow always" on a bash call, the prompt shows an editable suggested pattern: first 1–2 tokens of the command.

| Command | Suggested pattern |
|---|---|
| `git status -sb` | `^git status` |
| `npm run build` | `^npm run build` |
| `ls -la /tmp` | `^ls` |
| `python -m pytest tests/` | `^python -m pytest` |

For non-bash tools, "Allow always" just adds the tool name to the `tools` array.

### Skip mode (yolo)

- `--yolo` CLI flag at startup makes every `Check` return `Allow: true`.
- `/yolo` slash command toggles it mid-session (single `atomic.Bool`).
- TUI status bar shows `[YOLO]` in red while active.
- Subagents inherit the parent's yolo state at spawn time.
- Yolo never writes to the whitelist file.

### Approval prompt outcomes

`Allow once` · `Allow always (with editable pattern)` · `Deny` · `Deny + steer (write a message to the agent)`. See Section 7 for rendering.

### Failure modes

- Whitelist file missing → treated as empty.
- Malformed JSON → logged once, treated as empty.
- Bad regex pattern → logged once, that entry skipped, others still load.
- Concurrent writes (multiple gohome processes) → in-process mutex + `flock` on the whitelist file when writing.

---

## 5. Subagent mechanics

The `subagent` tool spawns a fresh, isolated agent goroutine that shares the parent's tool registry (minus `subagent` itself) and `Guard` instance, blocks the parent until done, and surfaces its events to the TUI under a new session ID.

### Tool input

```go
type subagentInput struct {
    Task         string `json:"task"`
    SystemPrompt string `json:"system_prompt,omitempty"`
}
```

### Spawn flow

1. Parent's agent goroutine calls `subagent.Execute(ctx, in, sink)`.
2. `Execute` creates a child `context.Context` from the parent's, a fresh session struct with a new ID, and a tool registry that excludes `subagent` (one-layer cap, enforced structurally).
3. A new goroutine runs the standard agent loop with: empty message history, the task as the first user message, the parent's `Guard` instance, and the parent's `Frontend`.
4. The parent's goroutine blocks on a result channel.
5. The TUI sees a `session_started` event, adds it to the focus list, surfaces an inline notification in the parent's view.
6. When the subagent's loop terminates (the LLM emits a non-tool-use stop), its final assistant text becomes the tool result returned to the parent.

### Recursion prevention

Belt-and-suspenders:
- The subagent's registry excludes `subagent` — the LLM literally does not see the tool.
- A `Session.Depth` field is incremented per spawn; a defensive check refuses if `Depth >= 1`.

### Session struct

```go
type Session struct {
    ID       string   // "main", "sub-1", "sub-2"
    Depth    int      // 0 = parent, 1 = subagent
    ParentID string   // empty for parent
    History  []Message
    CWD      string
}
```

### Cancellation and steering

- The approval prompt's `[4] Deny + steer` option universally lets the user reject a tool call with a steering message that becomes the `tool_result` for that call. Works for parent and subagent alike.
- `Ctrl+C` while focused on a subagent cancels the subagent's `context`. The subagent's tool result returned to the parent becomes `Result{IsError: true, Content: "Subagent cancelled by user"}` (or the steering text if provided via `/cancel <msg>`).

### Whitelist and yolo in subagents

- Subagents share the parent's `Guard` instance — same in-memory whitelist (including additions made during the subagent's run) and same yolo flag.
- "Allow always" entries added while focused on a subagent persist to the project whitelist file.

---

## 6. Session model and persistence

Append-only JSONL files, one per session (parent or subagent), grouped on disk by project, with metadata-rich first lines so resume listings are cheap.

### On-disk layout

```
~/.gohome/sessions/
  <project-slug>/                          # base(cwd) + "-" + sha1(cwd)[:6]
    2026-05-29-<session-id>.jsonl          # parent
    2026-05-29-<subagent-id>.jsonl         # each subagent its own file
```

### JSONL event schema

```jsonc
// First line — always session_start, carries metadata for cheap listings
{"type":"session_start","ts":"2026-05-29T10:42:13Z","id":"main","parentId":"","depth":0,
 "cwd":"/...","model":"claude-opus-4-7","endpoint":"local-anthropic"}

{"type":"user_message","ts":"...","content":[{"type":"text","text":"refactor X"}]}

{"type":"assistant_message","ts":"...","stopReason":"tool_use",
 "content":[{"type":"text","text":"..."},{"type":"tool_use","id":"tu_1","name":"read","input":{...}}]}

{"type":"tool_result","ts":"...","toolUseId":"tu_1","isError":false,"content":"..."}

{"type":"approval","ts":"...","toolUseId":"tu_1","outcome":"whitelisted"}
{"type":"approval","ts":"...","toolUseId":"tu_5","outcome":"allow_always","savedPattern":"^git status"}
{"type":"approval","ts":"...","toolUseId":"tu_8","outcome":"deny_steer","steerMessage":"..."}

{"type":"subagent_spawn","ts":"...","toolUseId":"tu_3","childId":"sub-1","task":"..."}
{"type":"subagent_done","ts":"...","toolUseId":"tu_3","childId":"sub-1","isError":false}

{"type":"session_end","ts":"...","reason":"user_quit"}
```

### Writer mechanics

- One `sessionWriter` per session, owning the file handle.
- Buffered channel (`chan event`, size 64) → background goroutine writes one line per event.
- `Sync()` on important events (`session_end`, `approval`); skipped on hot-path events.
- On crash, JSONL is naturally truncated but valid up to the last whole line.

### What gets written

- Every message, tool call, tool result, approval decision, subagent marker, model/endpoint change.

### What does not get written

- Streamed token deltas (only the assembled `assistant_message` at turn end).
- TUI focus-switch events.
- Transient non-conversation errors.

### Subagent persistence

Each subagent runs its own `sessionWriter` to its own JSONL file. Parent and child link via `ParentID` on the child's `session_start` and `childId` on the parent's `subagent_spawn`/`subagent_done`. Browsing a parent session, the TUI shows subagent rows expandable inline.

### Resume

- `gohome --resume` lists sessions for the current project, reading only first + last line of each file.
- Selected session is loaded fully, reconstructed in memory, and continues.
- Only parent sessions are resumable directly; subagents are viewable but not re-entrant.
- No branching, forking, or in-place editing of old turns in v1.

---

## 7. TUI shape

A single Bubble Tea program whose model holds a map of session timelines, renders the focused one in the main viewport, and overlays an approval prompt that gates input whenever the agent is waiting for a decision.

### Bubble Tea model

```go
type Model struct {
    sessions         map[string]*SessionView   // keyed by session ID
    order            []string                  // spawn order, for focus cycling
    focused          string                    // currently visible session
    input            textarea.Model
    viewport         viewport.Model
    activeApproval   *approvalPrompt           // nil when none
    pendingApprovals map[string]*approvalPrompt
    yolo             bool
    statusBar        statusBar
}

type SessionView struct {
    ID       string
    Depth    int
    Title    string
    Timeline []TimelineEntry
    InFlight bool
    Usage    Usage     // cumulative for this session
}
```

### Agent → TUI message types

```go
type sessionStartedMsg struct { session *Session }
type tokenDeltaMsg     struct { sessionID, text string }
type toolCallMsg       struct { sessionID string; call ToolCall }
type toolResultMsg     struct { sessionID string; result ToolResult }
type approvalReqMsg    struct { req ApprovalRequest; reply chan ApprovalDecision }
type usageUpdatedMsg   struct { sessionID string; usage Usage }
type turnDoneMsg       struct { sessionID string; stopReason string }
type sessionEndedMsg   struct { sessionID string }
```

The `reply chan` on `approvalReqMsg` is how the `Frontend` delivers the decision back to the blocked agent goroutine.

### Layout

```
┌─────────────────────────────────────────────────────────┐
│ Session: main  ◉  [sub-1 ●running]  [sub-2 ✓done]       │
├─────────────────────────────────────────────────────────┤
│  > refactor the auth flow                               │
│  • I'll start by reading the file.                      │
│    ▸ read foo.go  →  123 lines                          │
│  • now I'll spawn a subagent to investigate...          │
│    ▸ subagent sub-1  →  (running)                       │
├─────────────────────────────────────────────────────────┤
│ ⚠ [sub-1] needs approval — Ctrl+] to focus              │
├─────────────────────────────────────────────────────────┤
│ > _                                                     │
├─────────────────────────────────────────────────────────┤
│ main · opus-4-7 · ▓▓░░░░░░░░ 12.3k/200k (6%) · [YOLO]   │
└─────────────────────────────────────────────────────────┘
```

### Approval prompt overlay

```
┌─ Approve tool call — sub-1 ─────────────────────────────┐
│ Tool: bash                                              │
│ Command: rm -rf /tmp/cache                              │
│                                                         │
│  [1] Allow once                                         │
│  [2] Allow always   pattern:  ^rm -rf /tmp    (e edit)  │
│  [3] Deny                                               │
│  [4] Deny + steer  (message the agent)                  │
└─────────────────────────────────────────────────────────┘
```

When approvals queue across sessions, the focused session's approval is active. Pending approvals from other sessions appear as `⚠` markers in the session strip and a notification line. Focus-switching auto-promotes that session's approval.

### Keybindings (v1)

| Key | Action |
|---|---|
| `Enter` | Submit input |
| `Shift+Enter` | Newline |
| `Ctrl+C` (1×) | Cancel current LLM turn or approval prompt |
| `Ctrl+C` (2×) | Quit |
| `Ctrl+]` | Focus next session |
| `Ctrl+[` | Focus previous session |
| `Ctrl+L` | Clear viewport (history preserved) |
| `PgUp` / `PgDn` | Scroll viewport |
| `/` | Open slash command (inline autocomplete) |
| `1`–`4` | Pick option (approval prompt only) |
| `e` | Edit suggested pattern (approval prompt only) |
| `Esc` | Deny / close overlay |

### Slash commands (v1)

```
/new                  start a fresh session
/resume               list and pick a past session
/yolo                 toggle yolo mode
/endpoint <name>      switch endpoint mid-session
/model <name>         switch model mid-session
/cancel [message]     cancel focused session's current turn (with optional steering)
/tokens               open token usage detail overlay
/quit
```

### Streaming render

Token deltas append to the in-progress assistant message in `SessionView.Timeline`. The viewport re-renders only the last entry on each delta.

### Tool-call rendering

One line by default; `Enter` on the line toggles expansion.

```
▸ read foo.go  →  123 lines
▸ bash git status -sb  →  exit 0, 4 lines
▸ edit auth.go  →  1 replacement
```

### Token and context tracking

Status bar shows cumulative usage for the focused session against the endpoint's `contextWindow` (default 128,000 if unset):

```
main · opus-4-7 · ▓▓░░░░░░░░ 12.3k/200k (6%) · [YOLO]
```

Bar uses 10 cells. Green ≤50%, yellow 50–80%, red >80%. A small `▲` appears next to the bar after each turn for ~1s.

`Usage` flows through the LLM stream:

```go
type Usage struct {
    InputTokens      int
    OutputTokens     int
    CacheReadTokens  int  // Anthropic only
    CacheWriteTokens int  // Anthropic only
}
```

Populated on `turn_done` events. The agent accumulates `Usage` into `Session` and emits `usageUpdatedMsg` to the TUI.

`/tokens` overlay:

```
┌─ Token usage — main · opus-4-7 ───────────────────┐
│   Input tokens          11,234                    │
│   Output tokens          1,089                    │
│   Cache reads               12                    │
│   Cache writes             342                    │
│   ────────────────────────────                    │
│   Total                 12,323  /  200,000 (6%)   │
│                                                   │
│   Subagents (this session)                        │
│     sub-1               4,521                     │
│     sub-2               1,204                     │
│                                                   │
│   Esc to close                                    │
└───────────────────────────────────────────────────┘
```

Thresholds:
- 80%: yellow notification — `Context 80% full — consider /new or /resume into a fresh session.`
- 95%: red notification — `Context near limit — next turn may fail or truncate.`

### Theming

- One default theme (terminal-aware via `termenv`).
- Theme stored as a struct of `lipgloss.Style` fields.

---

## 8. Error handling and testing

### Error tiers

| Tier | Example | Policy |
|---|---|---|
| Wire failure (retryable) | LLM connection refused, SSE truncation, 5xx | Adapter retries up to 3 times with exponential backoff (250ms → 1s → 2s). On final failure, agent surfaces a turn-level error event; user sees a red error block and can `/retry` or type a new message. |
| Expected error | File not found, command exit non-zero, whitelist deny | Returned as `Result{IsError: true, Content: "..."}`. Becomes a `tool_result` block in the next LLM call. |
| Fatal | Config unreadable at startup, missing API key, panic in agent goroutine | Logged to `~/.gohome/logs/<date>.log`, agent goroutine exits cleanly, TUI surfaces a banner with the log path. |

Specific policies:

- `context.Cancelled` is never an error; propagate cleanly.
- LLM 4xx is not retried; surface a single-line error.
- `max_tokens` reached is a normal stop reason; TUI shows `(response truncated)`.
- Tool panics are caught by `defer recover()` in the dispatcher; converted to `Result{IsError: true, Content: "tool panicked: ..."}`. Stack trace logged.
- Whitelist write races: in-process mutex + `flock` on the file.
- Subagent failure returns to the parent as the `subagent` tool result with `IsError: true`.

### Logging

- Rolling log file at `~/.gohome/logs/<YYYY-MM-DD>.log`.
- Structured (one JSON line per entry: `{ts, level, session, msg, ...}`).
- Levels: `error` (always), `warn` (always), `info` (default), `debug` (env `GOHOME_DEBUG=1`).
- No remote telemetry.

### Testing strategy

1. **Unit tests** — Standard Go `testing`. `guard`, `tools`, `session`, `config` covered breadth-first.
2. **LLM adapter tests with recorded SSE fixtures** — `testdata/` per adapter holds captured SSE streams; tests replay through the adapter and assert the resulting `[]StreamEvent`. No network in CI.
3. **Agent loop tests with a fake LLM** — A test `Client` that emits scripted `StreamEvent`s. Drives turn loop, tool dispatch, guard interaction, subagent spawn/return.
4. **TUI snapshot tests via `teatest`** — Synthetic key sequences and `tea.Msg` values; final frame compared against `testdata/*.golden.txt`.
5. **E2E smoke test, opt-in only** — Behind `-tags=e2e` or `GOHOME_E2E=1`. Not in CI.

### CI shape

- `go vet`, `golangci-lint`, `go test ./...` (excludes `e2e` tag).
- Cross-build for `linux/amd64`, `darwin/arm64`, `darwin/amd64`, `windows/amd64`.
- Binary-size guard: `gohome` stays under ~25 MB stripped.

### Not done in v1

- No SSE-parser fuzzing.
- No benchmark suite.
- No coverage gates in CI.

---

## Future work

Tracked here so the v1 scope stays bounded. None of these block shipping.

- **Daemon mode** — second `Frontend` implementation over JSON-RPC. The seam is already in place.
- **Context compaction** — automatic + manual, with file tracking and summary entries (pi-style).
- **Reasoning / thinking tokens** as first-class blocks (preserved as text in v1).
- **Denylist** alongside the whitelist — to keep specific patterns prompted even if they would otherwise match an allow.
- **In-flight subagent steering** — inject a user message into a running subagent's conversation without cancelling it.
- **Cross-session search / history browsing UI.**
- **Markdown export** of a session transcript.
- **Custom themes.**
- **Per-subagent independent whitelist** — for stricter isolation.
- **Per-session model selection.**
- **Mouse support.**
- **Image rendering** (sixel / kitty graphics), once image-input tools exist.
- **Audit log** of approval decisions under `~/.gohome/audit.log`.
- **SSE-parser fuzzing**, benchmark suite, coverage gates.
