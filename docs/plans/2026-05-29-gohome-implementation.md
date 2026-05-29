# GoHome Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the `gohome` Go binary — a lightweight coding agent + Bubble Tea TUI with built-in tool-call guardrails and sequential subagents — to a runnable v1.

**Architecture:** Single binary. Each agent session runs in its own goroutine and talks to the TUI through a small `Frontend` interface (channels under the hood). Two thin LLM adapters (Anthropic-style + OpenAI-compatible) feed a common stream. Tools are pure execution units; the `Guard` wraps them with whitelist + approval. Sessions persist to append-only JSONL.

**Tech Stack:** Go 1.22+, `bubbletea` + `lipgloss` + `bubbles` (textarea, viewport), `termenv`, standard library for HTTP/SSE/JSON. Test harness: stdlib `testing`, `teatest`. No third-party LLM SDKs.

**Source of truth:** [`2026-05-29-gohome-design.md`](2026-05-29-gohome-design.md). Every task references its design section. If a question is not answered here, the design doc is authoritative; if the design doc is silent, ask the user before inventing.

**Working norms:**
- TDD: write the failing test, see it fail, implement, see it pass, commit.
- One commit per task. Use Conventional Commits (`feat:`, `test:`, `chore:`, `refactor:`, `fix:`).
- No emojis in code, commits, or docs.
- Each task is scoped to ≤ ~30 minutes of focused work. If it grows, split it.
- Do not run `git commit --no-verify`.
- `internal/agent` must not import `internal/tui` — verify with `go vet` style import checks in CI.

---

## Phase 0: Repo Bootstrap

### Task 0.1: Initialize Go module

**Files:**
- Create: `go.mod`
- Create: `.gitignore` (additions for `gohome` binary, `*.test`, `coverage.out`)
- Create: `README.md` (one paragraph; expand later)

**Steps:**

1. From repo root, run `go mod init github.com/<owner>/pi/gohome` (replace owner; confirm with user if unclear).
2. Append to `.gitignore`:
   ```
   /gohome
   *.test
   coverage.out
   .gohome/
   ```
3. Verify: `go version` prints ≥ go1.22. If not, install.
4. Commit: `chore(gohome): initialize go module`.

### Task 0.2: Add tooling configs

**Files:**
- Create: `.golangci.yml`
- Create: `.github/workflows/gohome-ci.yml`

**Steps:**

1. `.golangci.yml` enables: `govet`, `staticcheck`, `errcheck`, `ineffassign`, `unused`, `gofmt`, `goimports`. Disable `gochecknoinits` and `funlen` defaults.
2. `.github/workflows/gohome-ci.yml`: matrix on `ubuntu-latest`, `macos-latest`, `windows-latest`; steps = checkout, setup-go@v5 (go 1.22), `go vet ./gohome/...`, `golangci-lint run ./gohome/...`, `go test ./gohome/...`.
3. Run `go vet ./gohome/...` locally — should pass on empty module.
4. Commit: `chore(gohome): add lint + ci config`.

### Task 0.3: Package skeleton

**Files (all empty placeholder files with package decl):**
- Create: `gohome/cmd/gohome/main.go`
- Create: `gohome/internal/agent/agent.go`
- Create: `gohome/internal/llm/common/types.go`
- Create: `gohome/internal/llm/anthropic/client.go`
- Create: `gohome/internal/llm/openai/client.go`
- Create: `gohome/internal/tools/registry.go`
- Create: `gohome/internal/guard/guard.go`
- Create: `gohome/internal/session/session.go`
- Create: `gohome/internal/config/config.go`
- Create: `gohome/internal/tui/tui.go`

**Steps:**

1. Each file has `package <name>` and nothing else.
2. `cmd/gohome/main.go`: `package main` with `func main() { os.Exit(0) }`.
3. Run `go build ./gohome/...` — must succeed.
4. Run `go test ./gohome/...` — passes (no tests yet).
5. Commit: `chore(gohome): scaffold package layout`.

---

## Phase 1: Common Types (Design §1, §2)

### Task 1.1: `llm/common` core message types

**Files:**
- Modify: `gohome/internal/llm/common/types.go`
- Create: `gohome/internal/llm/common/types_test.go`

**Step 1 — Failing test:**

```go
package common_test

import (
    "encoding/json"
    "testing"

    "<module>/gohome/internal/llm/common"
)

func TestMessage_JSONRoundtrip(t *testing.T) {
    m := common.Message{
        Role: common.RoleAssistant,
        Content: []common.Block{
            {Kind: common.BlockText, Text: "hello"},
            {Kind: common.BlockToolUse, ToolUseID: "tu_1", ToolName: "read", InputJSON: `{"path":"a"}`},
        },
    }
    b, err := json.Marshal(m)
    if err != nil { t.Fatal(err) }
    var got common.Message
    if err := json.Unmarshal(b, &got); err != nil { t.Fatal(err) }
    if got.Role != common.RoleAssistant || len(got.Content) != 2 {
        t.Fatalf("roundtrip mismatch: %+v", got)
    }
}
```

**Step 2 — Run, see it fail** (`common.Message` undefined).

**Step 3 — Implement:**

```go
package common

type Role string

const (
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
    RoleTool      Role = "tool"
)

type BlockKind string

const (
    BlockText       BlockKind = "text"
    BlockToolUse    BlockKind = "tool_use"
    BlockToolResult BlockKind = "tool_result"
)

type Block struct {
    Kind        BlockKind `json:"kind"`
    Text        string    `json:"text,omitempty"`
    ToolUseID   string    `json:"toolUseId,omitempty"`
    ToolName    string    `json:"toolName,omitempty"`
    InputJSON   string    `json:"inputJson,omitempty"`   // accumulated JSON for tool_use
    ResultText  string    `json:"resultText,omitempty"`
    IsError     bool      `json:"isError,omitempty"`
}

type Message struct {
    Role    Role    `json:"role"`
    Content []Block `json:"content"`
}
```

**Step 4 — Run test:** `go test ./gohome/internal/llm/common/...` → PASS.

**Step 5 — Commit:** `feat(gohome/llm): define common message types`.

### Task 1.2: `llm/common` stream + usage types

**Files:**
- Modify: `gohome/internal/llm/common/types.go`
- Modify: `gohome/internal/llm/common/types_test.go` (add test)

**Step 1 — Failing test:** assert `StreamEvent{Kind: EventTextDelta, TextDelta: "x"}.Kind == EventTextDelta` and `(*Usage)(nil)` is valid zero state.

**Step 3 — Implement:**

```go
type EventKind string

const (
    EventTextDelta       EventKind = "text_delta"
    EventToolCallPartial EventKind = "tool_call_partial"
    EventToolCallDone    EventKind = "tool_call_done"
    EventTurnDone        EventKind = "turn_done"
    EventError           EventKind = "error"
)

type Usage struct {
    InputTokens      int `json:"inputTokens"`
    OutputTokens     int `json:"outputTokens"`
    CacheReadTokens  int `json:"cacheReadTokens,omitempty"`
    CacheWriteTokens int `json:"cacheWriteTokens,omitempty"`
}

type StreamEvent struct {
    Kind        EventKind
    TextDelta   string
    ToolCallID  string
    ToolName    string
    InputJSON   string
    StopReason  string
    Usage       *Usage
    Err         error
}
```

**Step 4–5:** test passes, commit `feat(gohome/llm): add stream event + usage types`.

### Task 1.3: `llm/common` request + tool def types

**Files:**
- Modify: `gohome/internal/llm/common/types.go`
- Modify test as above.

**Implement:**

```go
type ToolDef struct {
    Name         string          `json:"name"`
    Description  string          `json:"description"`
    InputSchema  json.RawMessage `json:"inputSchema"`
}

type Request struct {
    Model     string
    System    string
    Messages  []Message
    Tools     []ToolDef
    MaxTokens int
}

type Client interface {
    Stream(ctx context.Context, req Request) (<-chan StreamEvent, error)
}
```

Commit: `feat(gohome/llm): add request + client interface`.

---

## Phase 2: Config (Design §2)

### Task 2.1: Endpoint + Settings structs

**Files:**
- Modify: `gohome/internal/config/config.go`
- Create: `gohome/internal/config/config_test.go`

**Step 1 — Failing test:**

```go
func TestSettings_ParseEndpoint(t *testing.T) {
    raw := []byte(`{
      "endpoints": {
        "e1": {"wire":"anthropic","baseURL":"http://x","apiKeyEnv":"K","defaultModel":"m","contextWindow":200000}
      },
      "defaultEndpoint": "e1"
    }`)
    var s config.Settings
    if err := json.Unmarshal(raw, &s); err != nil { t.Fatal(err) }
    e, ok := s.Endpoints["e1"]
    if !ok || e.Wire != "anthropic" || e.ContextWindow != 200000 { t.Fatalf("got %+v", e) }
}
```

**Step 3 — Implement:**

```go
type Wire string

const (
    WireAnthropic Wire = "anthropic"
    WireOpenAI    Wire = "openai"
)

type Endpoint struct {
    Wire          Wire              `json:"wire"`
    BaseURL       string            `json:"baseURL"`
    APIKey        string            `json:"apiKey,omitempty"`
    APIKeyEnv     string            `json:"apiKeyEnv,omitempty"`
    DefaultModel  string            `json:"defaultModel"`
    ContextWindow int               `json:"contextWindow,omitempty"`
    Headers       map[string]string `json:"headers,omitempty"`
}

type Settings struct {
    Endpoints       map[string]Endpoint `json:"endpoints"`
    DefaultEndpoint string              `json:"defaultEndpoint"`
}
```

Commit: `feat(gohome/config): settings + endpoint types`.

### Task 2.2: Global + project merge

**Step 1 — Failing test:** verifies that `Load(globalPath, projectPath)` merges endpoint maps (project overrides matching keys) and project's `defaultEndpoint` wins if set.

**Step 3 — Implement** `func Load(globalPath, projectPath string) (Settings, error)`:
- Read both files (missing = empty struct, NOT error).
- Decode each; if either is malformed JSON, log and treat as empty.
- Merge: start from global, overlay project; project endpoints with the same key replace global's; if project has `defaultEndpoint`, use it.

**Step 4–5:** commit `feat(gohome/config): load + merge global and project settings`.

### Task 2.3: API key resolution

**Step 1 — Failing test:** `ResolveAPIKey(Endpoint{APIKey:"literal"})` returns `"literal"`; `ResolveAPIKey(Endpoint{APIKeyEnv:"VAR"})` reads the env var; both empty returns `("", ErrNoAPIKey)`.

**Step 3 — Implement** `var ErrNoAPIKey = errors.New("no API key configured")` + `func ResolveAPIKey(e Endpoint) (string, error)`.

Commit: `feat(gohome/config): resolve api key from literal or env`.

### Task 2.4: Default paths

**Files:** add `func DefaultGlobalPath() string` returning `~/.gohome/settings.json` (use `os.UserHomeDir`) and `func DefaultProjectPath(cwd string) string` returning `cwd + "/.gohome/settings.json"`.

Test: assert structure of returned paths on tmp dir. Commit: `feat(gohome/config): default settings paths`.

---

## Phase 3: Anthropic Adapter (Design §2)

### Task 3.1: Capture an SSE fixture

**Files:** Create `gohome/internal/llm/anthropic/testdata/simple_text.sse` — a real captured SSE stream from an Anthropic-styled endpoint for a short text-only response.

**Steps:**
1. Manually capture once (`curl -N` against the user's endpoint, save raw bytes).
2. Sanitize any auth info from headers (we only store the body).
3. Commit fixture: `test(gohome/anthropic): add simple text SSE fixture`.

**Note for engineer:** if you don't have a live endpoint, hand-author a fixture from the [Anthropic streaming docs](https://docs.claude.com/en/api/messages-streaming) format. The format is `event: <name>\ndata: <json>\n\n`.

### Task 3.2: Anthropic request builder

**Files:**
- Modify: `gohome/internal/llm/anthropic/client.go`
- Create: `gohome/internal/llm/anthropic/request_test.go`

**Step 1 — Failing test:** given a `common.Request`, `buildAnthropicBody(req)` returns JSON with fields `model`, `system`, `messages` (each user/assistant with the correct content blocks), `tools` (Anthropic shape with `input_schema`), `max_tokens`, `stream: true`.

**Step 3 — Implement** translation. Key transforms:
- `common.BlockText` → `{type:"text", text:...}`
- `common.BlockToolUse` (in an assistant message) → `{type:"tool_use", id, name, input: <parsed InputJSON>}`
- A `common.Message{Role: RoleTool}` with `BlockToolResult` → user message with `{type:"tool_result", tool_use_id, content, is_error}`

Commit: `feat(gohome/anthropic): translate request to wire format`.

### Task 3.3: SSE parser

**Files:**
- Create: `gohome/internal/llm/anthropic/sse.go`
- Create: `gohome/internal/llm/anthropic/sse_test.go`

**Step 1 — Failing test:** feed the fixture from 3.1 into `parseSSE(reader) <-chan sseFrame`; assert the sequence of `(event, data)` pairs matches.

**Step 3 — Implement** a minimal SSE reader: line-based, accumulate `data:` lines until blank line, emit `sseFrame{event, data}`. Use `bufio.Scanner` with a larger buffer (1 MB) to survive long token chunks.

Commit: `feat(gohome/anthropic): minimal sse parser`.

### Task 3.4: Event translator — text deltas

**Step 1 — Failing test:** drive `translateEvents(<-chan sseFrame) <-chan common.StreamEvent` with frames for `content_block_start` (text), three `content_block_delta` events with `text_delta`, one `content_block_stop`, one `message_stop`. Assert we get three `EventTextDelta` events and one `EventTurnDone`.

**Step 3 — Implement** the translator. State machine: track open content blocks by index; on `content_block_delta` of type `text_delta`, emit `EventTextDelta`.

Commit: `feat(gohome/anthropic): translate text-delta sse events`.

### Task 3.5: Event translator — tool_use accumulation

**Step 1 — Failing test:** drive translator with a `content_block_start` (tool_use), several `input_json_delta`s assembling `{"path":"foo"}`, then `content_block_stop`. Assert: zero `EventTextDelta`, one `EventToolCallDone` with `InputJSON == {"path":"foo"}`, name and id preserved.

**Step 3 — Implement** accumulation in the translator. Use a per-block buffer keyed by block index.

Commit: `feat(gohome/anthropic): accumulate tool_use input json deltas`.

### Task 3.6: Event translator — usage on message_stop

**Step 1 — Failing test:** message with `message_delta` carrying `usage` and `message_stop`; assert one `EventTurnDone` whose `Usage` is non-nil with correct `InputTokens`/`OutputTokens`/`CacheRead*`/`CacheWrite*`.

**Step 3 — Implement** Usage extraction.

Commit: `feat(gohome/anthropic): extract usage from message_delta`.

### Task 3.7: HTTP transport + Stream()

**Files:**
- Modify: `gohome/internal/llm/anthropic/client.go`
- Create: `gohome/internal/llm/anthropic/client_test.go` (uses `httptest.Server` to serve the fixture)

**Step 1 — Failing test:** spin up `httptest.Server` that returns the fixture for `POST /v1/messages`; create client pointing at that server; call `Stream(ctx, req)`; consume the channel; assert event sequence matches expected.

**Step 3 — Implement:**

```go
type Client struct {
    base    string
    apiKey  string
    model   string                // default model if request omits
    headers map[string]string
    hc      *http.Client
}

func New(e config.Endpoint, apiKey string) *Client { ... }

func (c *Client) Stream(ctx context.Context, req common.Request) (<-chan common.StreamEvent, error) {
    body := buildAnthropicBody(req)
    httpReq, _ := http.NewRequestWithContext(ctx, "POST", c.base+"/v1/messages", bytes.NewReader(body))
    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set("Anthropic-Version", "2023-06-01")
    httpReq.Header.Set("X-API-Key", c.apiKey)
    for k, v := range c.headers { httpReq.Header.Set(k, v) }
    resp, err := c.hc.Do(httpReq)
    if err != nil { return nil, err }
    if resp.StatusCode >= 400 {
        defer resp.Body.Close()
        b, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("anthropic %d: %s", resp.StatusCode, b)
    }
    out := make(chan common.StreamEvent, 16)
    go func() {
        defer resp.Body.Close()
        defer close(out)
        frames := parseSSE(resp.Body)
        for ev := range translateEvents(frames) {
            select {
            case out <- ev:
            case <-ctx.Done(): return
            }
        }
    }()
    return out, nil
}
```

Commit: `feat(gohome/anthropic): http transport + Stream()`.

### Task 3.8: Retry policy for wire failures

**Step 1 — Failing test:** server returns 503 twice then 200 with fixture; client succeeds on third attempt; record attempt count.

**Step 3 — Implement** a small retry wrapper (3 attempts; 250ms, 1s, 2s; only on connection errors and 5xx; never on 4xx; never on `context.Cancelled`).

Commit: `feat(gohome/anthropic): retry on transient failures`.

---

## Phase 4: OpenAI-Compatible Adapter (Design §2)

Tasks 4.1–4.8 mirror Phase 3 against the OpenAI Chat Completions wire format. Each task is structured the same way (failing test → impl → commit). Key differences:

- **Endpoint path:** `POST /chat/completions`.
- **Auth header:** `Authorization: Bearer <key>`.
- **Tool format:** `tools: [{type:"function", function:{name, description, parameters}}]`.
- **Stream format:** SSE with single `data: {...}` lines and a final `data: [DONE]`.
- **Tool-call accumulation:** `choices[0].delta.tool_calls[].function.arguments` is the streamed JSON; key on tool call index.
- **Usage:** included when `stream_options: {include_usage: true}` is sent; arrives on a final chunk before `[DONE]`.

Tasks:

- **4.1** Capture OpenAI SSE fixture.
- **4.2** OpenAI request builder + test.
- **4.3** OpenAI SSE parser (the `data: ` lines + `[DONE]` sentinel).
- **4.4** Event translator — text deltas (`choices[0].delta.content`).
- **4.5** Event translator — tool_calls accumulation.
- **4.6** Event translator — usage extraction.
- **4.7** Stream() against `httptest.Server`.
- **4.8** Retry policy (reuses Phase 3.8 logic — extract into `internal/llm/common/retry.go` here; refactor Phase 3 to share).

Each commit: `feat(gohome/openai): <component>`.

After 4.8, run `go test ./gohome/internal/llm/...` — all green.

---

## Phase 5: Client Factory + Endpoint Selection

### Task 5.1: Client factory

**Files:**
- Create: `gohome/internal/llm/factory.go`
- Create: `gohome/internal/llm/factory_test.go`

**Step 1 — Failing test:** `New(endpoint, apiKey)` returns an Anthropic client when `endpoint.Wire == "anthropic"`, OpenAI client otherwise; unknown wire returns error.

**Step 3 — Implement:**

```go
package llm

import (
    "fmt"
    "<module>/gohome/internal/config"
    "<module>/gohome/internal/llm/anthropic"
    "<module>/gohome/internal/llm/common"
    "<module>/gohome/internal/llm/openai"
)

func New(e config.Endpoint, apiKey string) (common.Client, error) {
    switch e.Wire {
    case config.WireAnthropic: return anthropic.New(e, apiKey), nil
    case config.WireOpenAI:    return openai.New(e, apiKey), nil
    default: return nil, fmt.Errorf("unknown wire: %q", e.Wire)
    }
}
```

Commit: `feat(gohome/llm): client factory by wire format`.

---

## Phase 6: Tools (Design §3)

### Task 6.1: Tool interface + Result

**Files:**
- Modify: `gohome/internal/tools/registry.go`
- Create: `gohome/internal/tools/tool.go`
- Create: `gohome/internal/tools/tool_test.go`

**Implement:**

```go
package tools

import (
    "context"
    "encoding/json"
)

type ProgressSink interface { Update(chunk string) }
type NullSink struct{}
func (NullSink) Update(string) {}

type Result struct {
    Content string
    IsError bool
    Details any
}

type Tool interface {
    Name() string
    Description() string
    InputSchema() json.RawMessage
    Execute(ctx context.Context, in json.RawMessage, sink ProgressSink) (Result, error)
}
```

Commit: `feat(gohome/tools): core interfaces`.

### Task 6.2: Registry

**Step 1 — Failing test:** create a fake tool; `r := NewRegistry(); r.Register(fake); got, ok := r.Get("fake")` — `ok == true`, `got == fake`. `r.Schemas()` returns one `common.ToolDef` matching the fake.

**Step 3 — Implement:**

```go
type Registry struct { m map[string]Tool }
func NewRegistry() *Registry { return &Registry{m: map[string]Tool{}} }
func (r *Registry) Register(t Tool) { r.m[t.Name()] = t }
func (r *Registry) Get(name string) (Tool, bool) { t, ok := r.m[name]; return t, ok }
func (r *Registry) Names() []string { ... sorted ... }
func (r *Registry) Schemas() []common.ToolDef { ... }
func (r *Registry) Without(name string) *Registry { ... } // returns a copy minus `name` (used by subagent spawn)
```

Commit: `feat(gohome/tools): registry`.

### Task 6.3: `read` tool

**Files:**
- Create: `gohome/internal/tools/read.go`
- Create: `gohome/internal/tools/read_test.go`

**Step 1 — Failing tests** (write all of them, see all fail):
- Reads file with line numbers.
- Honors `offset` and `limit`.
- Returns `IsError=true` when path doesn't exist.
- Default cap = 2000 lines.

**Step 3 — Implement** input as `{Path string; Offset, Limit *int}`. Use `bufio.Scanner`. Render as `<lineno>\t<content>\n`. Default limit 2000.

Commit: `feat(gohome/tools): read`.

### Task 6.4: `write` tool

Tests: writes new file; overwrites existing; errors if parent dir missing. Input `{Path, Content}`. Commit: `feat(gohome/tools): write`.

### Task 6.5: `edit` tool (no read-before-edit check yet)

Input `{Path, OldString, NewString, ReplaceAll *bool}`. Tests: unique replacement; `replace_all`; error on non-unique without `replace_all`; error on no match. Commit: `feat(gohome/tools): edit`.

### Task 6.6: Session-scoped "files read" tracking

The read-before-edit check must access session state. Add a small interface:

```go
type SessionState interface {
    MarkRead(path string)
    HasRead(path string) bool
    CWD() string
}
```

Pass `SessionState` via `context.Context` using a typed key in package `agent`. Test: helpers `agent.WithSession(ctx, s)` + `agent.SessionFrom(ctx)` round-trip a state.

Add this on the `agent` side (file: `gohome/internal/agent/ctx.go`). Tools import `agent` only for this key — acceptable because it's a small interface, not a Bubble Tea dependency. (Future cleanup: move to its own package if it grows.)

Commit: `feat(gohome/agent): session state in context`.

### Task 6.7: Wire `read` to mark + `edit` to check

Modify `read` to call `agent.SessionFrom(ctx).MarkRead(path)` on success. Modify `edit` to check `HasRead` first — if false, return `Result{IsError:true, Content:"edit: file must be read first"}`.

Tests: simulated session state confirms both behaviors. Commit: `feat(gohome/tools): read-before-edit enforcement`.

### Task 6.8: `bash` tool

**Files:** `gohome/internal/tools/bash.go` + test.

**Implement:**
- Input `{Command string; TimeoutMs *int; Cwd *string}`.
- Default timeout 120s; cap at 600s.
- Use `exec.CommandContext` with `/bin/sh` `-c` on Unix, `cmd` `/c` on Windows (use `runtime.GOOS`).
- Merge stdout+stderr. Stream lines through `sink.Update(line)` as they arrive (use `bufio.Scanner` on a `MultiWriter`).
- Final `Result.Content` = full captured output, prefixed with `exit <code>\n`.
- On timeout, kill process, return `IsError=true, Content="bash: timed out after Xms"`.

Test cases: `echo hello` returns exit 0 + hello; `false` returns exit 1; `sleep 5` with timeout 100ms returns timeout error; sink receives lines.

Commit: `feat(gohome/tools): bash`.

---

## Phase 7: Guard (Design §4)

### Task 7.1: Whitelist file format

**Files:**
- Modify: `gohome/internal/guard/guard.go`
- Create: `gohome/internal/guard/whitelist.go`
- Create: `gohome/internal/guard/whitelist_test.go`

**Implement:**

```go
type WhitelistFile struct {
    Tools []string `json:"tools"`
    Bash  []string `json:"bash"`
}
```

Test: JSON roundtrip. Commit: `feat(gohome/guard): whitelist file format`.

### Task 7.2: Compiled Whitelist + Allows()

**Step 1 — Failing tests:**
- `Allows("read", _)` returns true when `"read"` in `tools`.
- `Allows("bash", {"command":"git status -sb"})` returns true when pattern `"^git status"` present.
- `Allows("bash", {"command":"rm -rf /"})` returns false when no matching pattern.
- Patterns without `^` are auto-anchored.

**Step 3 — Implement:**

```go
type Whitelist struct {
    tools map[string]struct{}
    bash  []*regexp.Regexp
}

func Compile(global, project WhitelistFile) (*Whitelist, error) { ... merges, anchors, compiles ... }

func (w *Whitelist) Allows(tool string, inputJSON []byte) bool {
    if _, ok := w.tools[tool]; ok { return true }
    if tool == "bash" {
        var in struct{ Command string `json:"command"` }
        if json.Unmarshal(inputJSON, &in) == nil {
            for _, re := range w.bash {
                if re.MatchString(in.Command) { return true }
            }
        }
    }
    return false
}
```

Bad regex → log + skip that entry (use `log/slog` with a `slog.Warn`). Other entries still load.

Commit: `feat(gohome/guard): compiled whitelist with anchoring`.

### Task 7.3: Pattern suggestion heuristic

Test: `Suggest("bash", {"command":"git status -sb"})` returns `"^git status"`; `"npm run build foo"` → `"^npm run build"`; `"ls -la"` → `"^ls"`; non-bash tool returns empty.

Heuristic:
- Tokenize on whitespace.
- If tokens[0] is `git`, `npm`, `pnpm`, `yarn`, `go`, `python`, `cargo`, etc. (compile-time list), use first two tokens.
- Else use first token only.
- Prepend `^`.

Commit: `feat(gohome/guard): suggest pattern from command`.

### Task 7.4: ApprovalRequest / ApprovalDecision types

**Implement** (in `guard/guard.go`):

```go
type ApprovalOutcome string

const (
    AllowOnce   ApprovalOutcome = "allow_once"
    AllowAlways ApprovalOutcome = "allow_always"
    Deny        ApprovalOutcome = "deny"
    DenySteer   ApprovalOutcome = "deny_steer"
)

type ApprovalRequest struct {
    SessionID        string
    Tool             string
    Input            json.RawMessage
    Summary          string
    SuggestedPattern string
}

type ApprovalDecision struct {
    Outcome      ApprovalOutcome
    SavedPattern string
    SteerMessage string
}

type Decision struct {
    Allow        bool
    Reason       string
    SteerMessage string
}
```

Trivial commit: `feat(gohome/guard): approval types`.

### Task 7.5: Guard.Check() + Frontend dep

Define the `Frontend` interface locally to break the import cycle. (`internal/guard` will not import `internal/agent`; it just receives an interface.)

```go
type Frontend interface {
    RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalDecision, error)
}
```

The `agent` package will implement this interface (or wrap the real TUI Frontend). The TUI Frontend in `internal/tui` will satisfy both `agent.Frontend` and `guard.Frontend` by virtue of having `RequestApproval`.

**Test** (with a fake Frontend that records the request and returns a scripted decision):
- Yolo on → returns Allow without calling Frontend.
- Whitelist allows → returns Allow without calling Frontend.
- Otherwise calls Frontend; AllowOnce → Allow; AllowAlways → Allow + persists pattern; Deny → !Allow; DenySteer → !Allow + carries SteerMessage.

**Implement** `type Guard struct{...}` + `func (g *Guard) Check(ctx, sessionID, tool, input) (Decision, error)`.

Commit: `feat(gohome/guard): Check() with yolo + whitelist + frontend prompt`.

### Task 7.6: Yolo flag

`Guard.SetYolo(bool)` + `Yolo() bool` backed by `atomic.Bool`. Test concurrent access from multiple goroutines.

Commit: `feat(gohome/guard): yolo toggle`.

### Task 7.7: AddProject() with flock

**Files:**
- Create: `gohome/internal/guard/persist.go`
- Create: `gohome/internal/guard/persist_test.go`
- Create: `gohome/internal/guard/flock_unix.go` (build tag `!windows`)
- Create: `gohome/internal/guard/flock_windows.go` (build tag `windows`)

`AddProject(tool, pattern)`:
- If tool is `"bash"`, append `pattern` to project whitelist file's `bash`.
- Otherwise add `tool` to `tools` if not present.
- Acquires file lock on the whitelist file before reading + writing.
- Tolerates missing file (creates it).

Use `golang.org/x/sys/unix.Flock` for the Unix flock, `LockFileEx` for Windows.

Test (Unix only initially): concurrent `AddProject` calls from goroutines don't lose entries.

Commit: `feat(gohome/guard): persist allow-always with file lock`.

---

## Phase 8: Session + Persistence (Design §6)

### Task 8.1: Session struct + Messages mutator

**Files:**
- Modify: `gohome/internal/session/session.go`
- Create: `gohome/internal/session/session_test.go`

**Implement:**

```go
type Session struct {
    ID        string
    Depth     int
    ParentID  string
    CWD       string
    Model     string
    Endpoint  string
    History   []common.Message
    StartedAt time.Time

    readFiles map[string]struct{}
}

func NewSession(id, cwd, model, endpoint string) *Session { ... }

func (s *Session) MarkRead(path string) { ... }
func (s *Session) HasRead(path string) bool { ... }
func (s *Session) CWD() string { return s.cwd }
```

(`Session` implements `agent.SessionState` from Task 6.6.)

Tests: New + accessor behaviors. Commit: `feat(gohome/session): session struct`.

### Task 8.2: JSONL event types

**Files:** `gohome/internal/session/events.go` + test.

Define a tagged-union via a top-level struct + variant interfaces:

```go
type Event struct {
    Type string          `json:"type"`
    TS   time.Time       `json:"ts"`
    Raw  json.RawMessage `json:"-"`  // for unknown types
}

type SessionStart struct {
    ID, ParentID, CWD, Model, Endpoint string
    Depth                              int
}
type UserMessage struct{ Content []common.Block }
type AssistantMessage struct{ Content []common.Block; StopReason string; Usage *common.Usage }
type ToolResult struct{ ToolUseID, Content string; IsError bool }
type Approval struct{ ToolUseID string; Outcome string; SavedPattern, SteerMessage string }
type SubagentSpawn struct{ ToolUseID, ChildID, Task string }
type SubagentDone struct{ ToolUseID, ChildID string; IsError bool }
type SessionEnd struct{ Reason string }
```

Provide `func Encode(ev any) ([]byte, error)` that marshals to a flat JSON object including `type` and `ts`. Test roundtrips for each variant.

Commit: `feat(gohome/session): jsonl event types`.

### Task 8.3: sessionWriter

**Files:** `gohome/internal/session/writer.go` + test.

**Implement:**

```go
type Writer struct {
    f    *os.File
    ch   chan any
    done chan struct{}
}

func OpenWriter(path string) (*Writer, error) { /* create dir, open file O_APPEND|O_CREATE|O_WRONLY */ }

func (w *Writer) Emit(ev any)        { w.ch <- ev }
func (w *Writer) Close() error       { close(w.ch); <-w.done; return w.f.Close() }

// run() goroutine: read from ch, Encode, write, Sync on critical events
```

Critical events (sync after write): `SessionStart`, `SessionEnd`, `Approval`.

Test: open writer in tmp dir, emit a SessionStart + UserMessage + SessionEnd, close, read file back, assert 3 valid JSON lines with expected types.

Commit: `feat(gohome/session): jsonl writer with async sync`.

### Task 8.4: Project slug + path computation

`func ProjectSlug(cwd string) string` → `base(cwd) + "-" + sha1(cwd)[:6]`. Pure function. Test on a few inputs.

`func SessionPath(home, cwd, sessionID string, t time.Time) string` returns `home/sessions/<slug>/<YYYY-MM-DD>-<id>.jsonl`. Test.

Commit: `feat(gohome/session): on-disk path layout`.

### Task 8.5: Resume listing

`func List(home, cwd string) ([]Listing, error)` returns:

```go
type Listing struct {
    Path       string
    ID         string
    StartedAt  time.Time
    LastActive time.Time
    Title      string  // truncated from first user_message
    Depth      int     // skip subagents in listing UI
}
```

Reads only first + last line of each file. Test with synthetic JSONL files.

Commit: `feat(gohome/session): list resumable sessions`.

### Task 8.6: Resume load

`func Load(path string) (*Session, []common.Message, error)`. Reads the whole file, reconstructs `*Session` from `SessionStart`, builds `[]common.Message` from `UserMessage` + `AssistantMessage` + assistant `BlockToolUse` + matching `ToolResult` (as user role with `BlockToolResult`).

Test: write a fixture JSONL, load, assert reconstructed history matches.

Commit: `feat(gohome/session): load and reconstruct from jsonl`.

---

## Phase 9: Agent Loop (Design §1, §5)

### Task 9.1: Frontend interface in `agent`

**Files:** modify `gohome/internal/agent/agent.go`.

```go
package agent

type Event struct {
    Kind        EventKind
    SessionID   string
    TextDelta   string
    ToolCallID  string
    ToolName    string
    InputJSON   string
    Result      *ToolResult
    Usage       *common.Usage
    StopReason  string
    Err         error
}

type EventKind string

const (
    EventTokenDelta     EventKind = "token_delta"
    EventToolCallStart  EventKind = "tool_call_start"
    EventToolCallDone   EventKind = "tool_call_done"
    EventToolResult     EventKind = "tool_result"
    EventUsageUpdated   EventKind = "usage_updated"
    EventTurnDone       EventKind = "turn_done"
    EventSessionStarted EventKind = "session_started"
    EventSessionEnded   EventKind = "session_ended"
    EventError          EventKind = "error"
)

type Frontend interface {
    Emit(sessionID string, ev Event)
    RequestApproval(ctx context.Context, req guard.ApprovalRequest) (guard.ApprovalDecision, error)
    AwaitUserInput(ctx context.Context, sessionID string) (string, error)
}
```

Commit: `feat(gohome/agent): frontend interface and event types`.

### Task 9.2: Agent struct + dependencies

```go
type Agent struct {
    Client   common.Client
    Tools    *tools.Registry
    Guard    *guard.Guard
    Frontend Frontend
    Writer   *session.Writer
    System   string
}
```

Trivial. Commit: `feat(gohome/agent): agent struct`.

### Task 9.3: Single-turn implementation

`func (a *Agent) Turn(ctx context.Context, sess *session.Session) (stopReason string, _ error)`.

Logic:
1. Build `common.Request{Model: sess.Model, System: a.System, Messages: sess.History, Tools: a.Tools.Schemas(), MaxTokens: ...}`.
2. `events, err := a.Client.Stream(ctx, req)`.
3. Buffer up assistant message blocks as events arrive.
4. On `EventTextDelta`, forward to Frontend; append to in-progress text block.
5. On `EventToolCallDone`, finalize a `BlockToolUse` in the assistant message.
6. On `EventTurnDone`, capture stopReason + usage; close out the assistant message; persist it via writer; forward `EventTurnDone` to Frontend.

Test with a fake `common.Client` that emits a scripted event stream; assert the writer received the right Encoded events, the Frontend got the right Events, and `sess.History` has a new assistant message.

Commit: `feat(gohome/agent): single turn streaming`.

### Task 9.4: Tool dispatch loop

Test: scripted client returns a text + a tool_use; agent calls Guard (fake returns Allow), executes the tool via the registry (fake tool returns content), appends a tool_result user message, and runs another turn.

Implement `func (a *Agent) Run(ctx context.Context, sess *session.Session) error`:
- Loop `Turn()`.
- After each turn, for each `BlockToolUse` in the last assistant message:
  - Call `a.Guard.Check(ctx, sess.ID, name, input)`. If `!Allow`: synthesize `tool_result` with the deny reason or steer message; persist `Approval` event.
  - If allowed: run tool via registry; persist `Approval` (whitelisted/once/always) and `ToolResult`. Send `EventToolResult` to Frontend.
- After all tool_results appended to history, run another Turn().
- Loop ends when last turn's stopReason is not `"tool_use"`.

Commit: `feat(gohome/agent): tool dispatch loop`.

### Task 9.5: Error handling tiers

- Catch panics in tool execution with `defer recover()` → return `Result{IsError: true, Content: "tool panicked: ..."}`. Test with a tool that panics.
- LLM `Stream` returning a 4xx error: surface as `EventError`, abort run, log.
- Retry policy lives in adapters (already in place). Anything reaching `Run` is fatal-for-this-turn.

Commit: `feat(gohome/agent): error tiers + panic recovery`.

### Task 9.6: Context cancellation

Test: cancel `ctx` mid-turn; assert `Run` returns with `context.Cancelled` and the writer was closed cleanly (final `SessionEnd` emitted).

Implement: on `ctx.Done()`, drain the in-flight event channel, emit a `EventTurnDone{StopReason:"cancelled"}` to Frontend, persist `SessionEnd{Reason:"cancelled"}`, return `ctx.Err()`.

Commit: `feat(gohome/agent): clean cancellation`.

---

## Phase 10: Subagents (Design §5)

### Task 10.1: subagent tool skeleton

**Files:** `gohome/internal/tools/subagent.go` + test.

The subagent tool needs to construct another Agent and run it. To avoid an import cycle (`tools` → `agent` → `tools`), define a small interface in `tools`:

```go
type SubagentSpawner interface {
    Spawn(ctx context.Context, task, systemPrompt string, parentSessionID string) (resultText string, isError bool, err error)
}
```

A tool factory in `tools.NewSubagentTool(spawner)` constructs the tool. The agent package will provide the `SubagentSpawner` implementation in 10.2.

Test: a fake spawner that asserts it was called with the right task; tool returns its result.

Commit: `feat(gohome/tools): subagent tool that delegates to spawner`.

### Task 10.2: Agent.Spawn() implementation

**Files:** `gohome/internal/agent/spawn.go` + test.

`Agent.Spawn(ctx, task, sysprompt, parentSessionID) (string, bool, error)`:
1. Generate new session ID (`sub-1`, `sub-2`, ... incremented globally per parent).
2. Construct fresh `*session.Session` with `Depth=1`, `ParentID=parentSessionID`, same CWD/Model/Endpoint.
3. Open new `session.Writer` to subagent's JSONL path.
4. Build a fresh `Agent` *copy* of the parent that:
   - Shares `Client`, `Guard`, `Frontend`.
   - Has `Tools = parent.Tools.Without("subagent")`.
   - Has its own `Writer` pointing at the subagent's JSONL.
5. Push the task as the first user message into subagent's `sess.History`.
6. Emit `EventSessionStarted` on the parent's Frontend with the new session.
7. Run `subAgent.Run(ctx, sess)`.
8. Read the last assistant message's text from `sess.History`; return it as `resultText`.
9. Persist `SubagentSpawn` on the parent writer (the caller — the subagent tool's wrapper — does this; see 10.3).

Defensive check: if parent's `sess.Depth >= 1`, return error.

Test: fake LLM client scripted to spawn a subagent that itself returns a text response; assert parent's tool result equals subagent's final text.

Commit: `feat(gohome/agent): spawn isolated subagent`.

### Task 10.3: Wire subagent persistence on parent side

In `Agent.Run`'s tool dispatch loop, when the tool name is `"subagent"` and execution succeeds, emit a `SubagentSpawn` event to the parent's writer before tool execution and a `SubagentDone` event after.

Test: parent JSONL contains both events; subagent JSONL exists with its own messages.

Commit: `feat(gohome/agent): persist subagent spawn/done markers`.

### Task 10.4: Wire registration in Agent constructor

Update `Agent`'s construction site (will be in `cmd/gohome` in Phase 12) to register `subagent` tool with itself as the spawner. Until then, add `agent.NewWithSubagent()` helper that does the wiring + test.

Commit: `feat(gohome/agent): register subagent tool via spawner`.

---

## Phase 11: TUI (Design §7)

The TUI is built in slices. Each task adds rendering or behavior to the Bubble Tea Model. Use `teatest` for snapshot tests; update snapshots with `go test ./... -update`.

### Task 11.1: Skeleton model

**Files:**
- Modify `gohome/internal/tui/tui.go`
- Create `gohome/internal/tui/tui_test.go`
- Create `gohome/internal/tui/style/style.go`

Implement a minimal `tea.Model` with empty Init/Update/View. `View()` returns the literal string `"gohome\n"`. Add a `teatest`-based test that asserts the initial render contains "gohome".

Commit: `feat(gohome/tui): skeleton bubble tea model`.

### Task 11.2: SessionView + Timeline entry

Add `SessionView` and `TimelineEntry` per Design §7. `View()` renders the focused session's last entries as plain text.

Commit: `feat(gohome/tui): session view + timeline`.

### Task 11.3: Agent → TUI message routing

The TUI's `Frontend.Emit(sessionID, ev)` translates each `agent.Event` into a `tea.Msg` and sends it via `Program.Send(...)`. Define a single internal type:

```go
type agentEventMsg struct { sessionID string; ev agent.Event }
```

Inside `Update`, switch on `ev.Kind` and update the corresponding `SessionView`.

Test (via teatest): drive `agentEventMsg{ev: token_delta "hi"}` → final view contains "hi".

Commit: `feat(gohome/tui): translate agent events to tea messages`.

### Task 11.4: textarea input + Enter submit

Use `bubbles/textarea`. On Enter, send the input to the focused agent session via an injected `chan string`. The agent goroutine reads from this channel when it needs user input (the `Frontend.AwaitUserInput` implementation).

Test: simulate keystrokes via teatest, assert the channel receives the typed string.

Commit: `feat(gohome/tui): input textarea + submit`.

### Task 11.5: Viewport scrollback

Use `bubbles/viewport`. Append timeline entries to a string buffer; PgUp/PgDn scroll.

Commit: `feat(gohome/tui): viewport scrollback`.

### Task 11.6: Status bar

Render `<sessionID> · <model> · <usage> · [YOLO?]` where `<usage>` is rendered by 11.7.

Commit: `feat(gohome/tui): status bar`.

### Task 11.7: Token progress bar

Pure function `func progressBar(used, total int, width int) string` returning 10-cell bar with thresholds (green/yellow/red via Lip Gloss). Test with several ratios.

Wire into status bar. Commit: `feat(gohome/tui): token progress bar`.

### Task 11.8: Session strip + focus switching

Render top-line `Session: <focused>  ◉  [sub-1 ●running]  ...`. Implement Ctrl+] / Ctrl+[ keybindings to cycle `focused` through `order`.

Test: spawn `sessionStartedMsg{ID:"sub-1"}` then simulate Ctrl+], assert focus moved.

Commit: `feat(gohome/tui): session strip + focus cycling`.

### Task 11.9: Approval prompt overlay — basic

Implement `approvalPrompt` struct, rendered as a box that replaces the input region when `activeApproval != nil`. Render Tool/Command/options 1/2/3/4. Keys 1/3 work; 2 and 4 stub.

Wire `Frontend.RequestApproval` to enqueue an `approvalPrompt` and block until the user picks. Use a per-request `reply chan` carried in the `agentEventMsg`.

Test: simulate the prompt + key "1" → reply channel receives `AllowOnce`.

Commit: `feat(gohome/tui): approval prompt overlay basics`.

### Task 11.10: Pattern editor (key `e` + key `2`)

Pressing `2` saves the suggested pattern as-is. Pressing `e` enters an inline edit mode (a tiny textinput preloaded with the pattern); Enter confirms, Esc reverts.

Test: simulate `e`, type ` -- extra`, Enter, then `2` → reply has `AllowAlways` with edited pattern.

Commit: `feat(gohome/tui): editable allow-always pattern`.

### Task 11.11: Deny + steer (key `4`)

Pressing `4` opens a one-shot textarea for the steering message. Enter sends `DenySteer` + message.

Test as above.

Commit: `feat(gohome/tui): deny + steer flow`.

### Task 11.12: Notification line

When `activeApproval` is in a non-focused session or another session is in flight, render `⚠ [sub-1] needs approval — Ctrl+] to focus` above the input.

Test: drive an approval message for sub-1 while focused on main; assert notification rendered.

Commit: `feat(gohome/tui): cross-session notification line`.

### Task 11.13: Tool call rendering + expansion

Each `EventToolCallDone` and matching `EventToolResult` collapse to a single line:

```
▸ read foo.go  →  123 lines
```

Pressing Enter while cursor is on a tool-call line toggles expansion to show full content. Cursor navigation: Up/Down move between timeline entries when input is empty.

Commit: `feat(gohome/tui): tool call lines + expansion`.

### Task 11.14: Slash command palette

Implement `/` opening an inline autocomplete (matched against a static list). Commands `/new /resume /yolo /endpoint /model /cancel /tokens /quit`. For now wire only `/yolo` and `/quit` to actions; others stub with "not implemented" message in status bar.

Commit: `feat(gohome/tui): slash command palette`.

### Task 11.15: /tokens overlay

Render the breakdown overlay from Design §7. Closed on Esc. Test snapshot.

Commit: `feat(gohome/tui): tokens overlay`.

### Task 11.16: Context-fullness warnings

When usage ratio crosses 80% or 95%, emit a one-time warning into the notification line. Test by driving usage messages over the threshold.

Commit: `feat(gohome/tui): context fullness warnings`.

### Task 11.17: Snapshot suite

Add `testdata/snapshots/*.golden.txt` for: empty, single user message, after one assistant turn, with approval prompt, with subagent strip, with /tokens overlay. Run `go test -run TestSnapshots ./gohome/internal/tui/...`.

Commit: `test(gohome/tui): golden-file snapshot suite`.

---

## Phase 12: Wire It Up (Design §1)

### Task 12.1: cmd/gohome — flag parsing

```go
// gohome/cmd/gohome/main.go
var (
    endpointName = flag.String("endpoint", "", "endpoint name override")
    modelName    = flag.String("model", "", "model override")
    yolo         = flag.Bool("yolo", false, "disable all approval prompts")
    resume       = flag.Bool("resume", false, "resume a past session")
)
```

Test by running `go run ./gohome/cmd/gohome --help`; expect the flags listed.

Commit: `feat(gohome/cmd): cli flags`.

### Task 12.2: cmd/gohome — wire dependencies

In `main`:

1. Load config (Phase 2).
2. Resolve endpoint (CLI > config default). Resolve API key.
3. Build LLM client (Phase 5 factory).
4. Build whitelist (`guard.Load(home, project)`).
5. Build `guard.Guard{Whitelist, Frontend: tuiFrontend}`. Set yolo from flag.
6. Build tool registry: register read/write/edit/bash. Subagent registered last (Task 10.4 helper) with the parent Agent as spawner.
7. Construct `session.Session` (new ID via `nanoid` or short ULID — use `crypto/rand` to derive an 8-char base32 ID; no third-party dep).
8. Open `session.Writer` for that session.
9. Construct `tui.New(...)` returning a `*tea.Program` and a `Frontend` impl that talks back to it.
10. Construct `agent.Agent{...}` with all the pieces.
11. Start `tea.Program` in main goroutine; agent runs in a goroutine driven by user input from the TUI.

Test (manual): `go run ./gohome/cmd/gohome --endpoint <name>` against a real endpoint, verify TUI renders and a simple `hi` message produces an assistant reply.

Commit: `feat(gohome/cmd): wire config + guard + agent + tui`.

### Task 12.3: --resume flow

When `--resume` is set, before constructing a fresh session, list resumable sessions (Phase 8.5), show a selector overlay in the TUI, on selection call `session.Load`, hand the loaded session to the agent.

Test: scripted listing returns 2 entries; selector picks index 1; loaded session's ID matches.

Commit: `feat(gohome/cmd): --resume flow`.

### Task 12.4: Logging setup

Configure `slog` once in main:
- Output: `~/.gohome/logs/YYYY-MM-DD.log`
- Format: JSON.
- Level: info default; debug if `GOHOME_DEBUG=1`.

Test: assert log file is created and contains a `gohome started` info line on launch.

Commit: `feat(gohome/cmd): structured logging`.

### Task 12.5: Graceful shutdown

On SIGINT / Bubble Tea quit, cancel root context; wait for agent goroutine; close all writers; flush logs.

Test: SIGINT to a running process exits cleanly within 1s.

Commit: `feat(gohome/cmd): graceful shutdown`.

---

## Phase 13: Polish

### Task 13.1: CI workflow finalization

Update `.github/workflows/gohome-ci.yml` to:
- Run `go vet`, `golangci-lint`, `go test ./gohome/...`.
- Cross-build for `linux/amd64`, `darwin/arm64`, `darwin/amd64`, `windows/amd64`.
- Upload binaries as artifacts.
- Binary-size guard: after build, run `du -k gohome` and fail if > 25600 KB (25 MB).

Commit: `ci(gohome): cross-build + size guard`.

### Task 13.2: Architecture seam check

Add a test that fails if `internal/agent` (or any of `llm`, `tools`, `guard`, `session`) imports `internal/tui`:

```go
// gohome/internal/agent/seam_test.go
func TestNoTUIImport(t *testing.T) {
    // walk imports of internal/agent (and subpackages), assert none start with .../internal/tui
}
```

Use `go/packages` or `go list -json ./gohome/internal/agent/...`. Run in CI.

Commit: `test(gohome): enforce agent/tui seam`.

### Task 13.3: E2E smoke test (opt-in)

`gohome/test/e2e/smoke_test.go` behind `//go:build e2e`. Runs `gohome` against `GOHOME_E2E_ENDPOINT` env var with a fixed prompt; asserts that the response contains a stable substring.

Commit: `test(gohome): opt-in e2e smoke test`.

### Task 13.4: README

Replace the placeholder with a real README:
- One-paragraph pitch.
- Install (`go install ./gohome/cmd/gohome@latest` or build instructions).
- Quickstart: example `settings.json`, run command, screenshot (text snippet).
- Keybindings table (from Design §7).
- Slash commands.
- Where files live (`~/.gohome/`, `./.gohome/`).
- "What's not in v1" / "future work" section (from Design future-work list).

Commit: `docs(gohome): README`.

### Task 13.5: Future-work tracker

Create `gohome/FUTURE.md` listing the future-work items from Design §"Future work", each with a one-liner about the seam or hook the v1 design preserves for it.

Commit: `docs(gohome): future-work tracker`.

---

## Verification before "v1 done"

Run, in order, and confirm:

1. `go vet ./gohome/...` — clean.
2. `golangci-lint run ./gohome/...` — clean.
3. `go test ./gohome/...` — all pass.
4. `go build ./gohome/cmd/gohome` — binary built.
5. `du -k gohome` — under 25 MB.
6. Manual: launch `gohome` against a configured endpoint; verify:
   - Type a prompt → streamed assistant reply.
   - Ask agent to run a bash command → approval prompt appears; "Allow always" with default pattern persists to `./.gohome/whitelist.json`.
   - Ask agent to spawn a subagent → focus strip shows new session; Ctrl+] focuses it; subagent JSONL exists.
   - Approval inside subagent works.
   - `/yolo` toggles, status bar updates.
   - `/tokens` overlay opens.
   - SIGINT exits cleanly; JSONL valid.

Only after all six pass: tag `v0.1.0`, open release notes referencing this plan.

---

## Execution Handoff

Plan complete and saved to `docs/plans/2026-05-29-gohome-implementation.md`. Two execution options:

**1. Subagent-Driven (this session)** — I dispatch a fresh subagent per task, review the diff between tasks, fast iteration. Good for staying in this conversation and catching design drift early.

**2. Parallel Session (separate)** — open a new session in a worktree, use the executing-plans skill for batch execution with checkpoints. Better for long uninterrupted work.

Which approach?
