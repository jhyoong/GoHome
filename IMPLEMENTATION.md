# Agent Chat — Implementation Specification

## Overview

A minimal, self-hosted AI agent chat interface built in Go. It connects to a local OpenAI-compatible inference endpoint (llama.cpp, OMLX), supports tool use with an interactive approval system, MCP client integration, and persistent sessions. It runs as a single binary serving a lightweight web UI accessible over the local network, including from headless machines.

---

## Core Principles

- **YAGNI**: Build only what is described here. No plugin systems, no auth layers, no multi-user support, no theming. Add later if needed.
- **KISS**: Flat package structure where possible. No abstractions until a second use case demands one. No generics unless the alternative is significantly worse.
- **Single binary**: The Go binary embeds all frontend assets. No external runtime dependencies. Deploy by copying one file.
- **Cross-platform**: Must work on Linux, macOS, and Windows (PowerShell). No OS-specific code outside build tags where unavoidable (e.g., shell execution differences).

---

## Requirements

### Functional

1. **Chat interface** — Send messages, receive streamed responses from a local LLM.
2. **Tool execution** — The LLM can request tool calls. Tools execute locally on the server.
3. **Tool approval** — Each tool call requires explicit user approval via the web UI before execution, unless whitelisted.
4. **Session management** — Create, list, resume, and delete chat sessions. Full conversation history persisted.
5. **MCP client** — Connect to external MCP servers, discover their tools, and make them available to the agent.
6. **Remote access** — Optionally serve the UI on `0.0.0.0` so any device on the LAN can access it (default: `127.0.0.1`).

### Non-Functional

1. **Minimal frontend** — Preact + TypeScript, built with esbuild, no framework beyond Preact.
2. **No build toolchain at runtime** — Frontend is pre-built and embedded via `go:embed`.
3. **SQLite for persistence** — No external database. Single file, zero config.
4. **Streaming** — LLM responses stream token-by-token to the UI over WebSocket.
5. **Graceful shutdown** — Clean up MCP connections, close DB, drain active requests on SIGINT/SIGTERM.

---

## Architecture

### System Diagram

```
┌─────────────────────────────────────────────────────┐
│                    Go Binary                        │
│                                                     │
│  ┌──────────┐   ┌───────────┐   ┌───────────────┐  │
│  │  HTTP     │   │  Agent    │   │  Tool         │  │
│  │  Server   │◄─►│  Loop     │◄─►│  Registry     │  │
│  │  + WS     │   │           │   │  (local+MCP)  │  │
│  └────┬─────┘   └─────┬─────┘   └───────┬───────┘  │
│       │               │                 │           │
│  ┌────┴─────┐   ┌─────┴─────┐   ┌──────┴────────┐  │
│  │ Embedded │   │  LLM      │   │  MCP Client   │  │
│  │ Frontend │   │  Client   │   │  (stdio/SSE)  │  │
│  └──────────┘   └─────┬─────┘   └───────────────┘  │
│                       │                             │
│                 ┌─────┴─────┐                       │
│                 │  SQLite   │                       │
│                 │  Store    │                       │
│                 └───────────┘                       │
└─────────────────────────────────────────────────────┘
         │                            │
         ▼                            ▼
   Browser (LAN)              Local LLM endpoint
                              (llama.cpp / OMLX)
```

### Component Responsibilities

**HTTP Server + WebSocket** (`internal/server/`)
- Serves embedded static files for the frontend.
- Upgrades `/ws` to a WebSocket connection. The frontend includes a `tab` query parameter (a UUID generated on page load) in the upgrade URL to identify the browser tab.
- Exposes REST endpoints for session CRUD (`GET /api/sessions`, `POST /api/sessions`, `DELETE /api/sessions/:id`).
- Maintains a connection registry keyed by tab ID. Only the tab that sent a user message receives the corresponding approval requests and token stream. Other tabs receive session list updates only.

**Agent Loop** (`internal/agent/`)
- Receives a user message, session ID, and the originating tab ID.
- Sends conversation history to the LLM.
- If the LLM response contains tool calls: routes each through the approval broker, executes approved tools, appends results to history, and re-sends to the LLM.
- Repeats until the LLM returns a plain text response (no tool calls).
- Streams final text tokens to the frontend via WebSocket as they arrive.
- All persistence (saving messages, tool results) happens here.

**LLM Client** (`internal/llm/`)
- Speaks the OpenAI `/v1/chat/completions` API.
- Supports streaming (SSE) and non-streaming modes.
- Sends the `tools` array derived from the tool registry.
- Handles streamed tool call arguments via buffering (see "Tool Call Streaming Strategy" below).
- Configurable: endpoint URL, model name, temperature, max_tokens.
- No retry logic. If the endpoint is down, surface the error to the user.

**Tool Registry** (`internal/tools/`)
- Thread-safe registry holding all available tools (local + MCP-sourced), protected by a `sync.RWMutex`.
- Each tool implements a single interface.
- Local tools are registered explicitly at startup (no `init()` magic).
- MCP tools are registered dynamically when MCP servers connect, and de-registered on disconnect.
- `Register()` returns an error if a tool with the same name already exists, preventing silent overwrites.
- Produces the `tools` JSON array for the LLM client.

**Approval Broker** (`internal/approval/`)
- When a tool call needs approval: generates a request ID, sends approval request to the frontend via the outbound channel, and blocks on a per-request channel in a `select` alongside the context and timeout.
- The frontend displays the tool name, parameters, and allow/deny buttons.
- On user response: the dispatcher routes the response to the correct per-request channel, unblocking the broker.
- Whitelist check happens before the WebSocket round-trip. If the tool matches a whitelist rule, auto-approve immediately.
- Timeout: if no response within 5 minutes, deny by default. Configurable.
- On context cancellation (e.g., shutdown), the `select` unblocks and returns an error.

**MCP Client** (`internal/mcp/`)
- Reads MCP server definitions from config.
- Connects via stdio (spawns subprocess) or SSE (HTTP connection).
- On connect: calls `initialize`, then `tools/list` to discover available tools.
- Wraps each MCP tool as a local `Tool` interface implementation that delegates `Execute()` to `tools/call` over the MCP protocol.
- On `Register()` collision: skips the conflicting tool, logs a warning, and continues registering the remaining tools from that server. The MCP server connection is not failed.
- On connection failure at startup: log a warning, skip the server, continue. Do not block startup.
- On mid-session disconnect: de-register tools, log an error, notify the frontend via WebSocket error message.
- No auto-reconnect in v1.

**Session Store** (`internal/session/`)
- SQLite database, single file at a configurable path (default: `~/.agent-chat/data.db`, resolved to an absolute path at config parse time).
- Schema auto-migrated on startup via embedded SQL.
- Three tables: `sessions`, `messages`, `tool_results`.
- All writes happen through a single `*sql.DB` instance. No ORM.
- On open: sets `journal_mode=WAL`, `busy_timeout=5000`, `synchronous=NORMAL`, and `foreign_keys=ON`.

**Config** (`internal/config/`)
- Single YAML file, path specified via `--config` flag or default `~/.agent-chat/config.yaml`.
- Parsed once at startup into a Go struct. No hot-reloading.
- All paths containing `~` are resolved to absolute paths at parse time using `os.UserHomeDir()`. No other code needs to handle tilde expansion.

---

## Goroutine Model (per WebSocket Connection)

Each WebSocket connection spawns four goroutines, communicating via channels:

```
┌──────────┐         ┌─────────────┐         ┌──────────┐
│ wsReader  │──inbound──►│ dispatcher  │         │ wsWriter │
└──────────┘         └──────┬──────┘         └────▲─────┘
                            │                      │
              ┌─────────────┼──────────────────────┤
              │             │                      │
              ▼             ▼                      │
        ┌──────────┐  ┌───────────┐               │
        │ approval │  │ agentLoop │───outbound─────┘
        │ broker   │  │           │
        └──────────┘  └───────────┘
```

1. **wsReader**: Reads from the WebSocket, deserializes JSON, pushes to the `inbound` channel. Exits on WS close or context cancellation. Never blocks on anything except reading the next message.
2. **wsWriter**: Reads from the `outbound` channel, writes to the WebSocket. Single writer prevents needing a mutex on WS writes.
3. **dispatcher**: Reads from `inbound`, routes by message type:
   - `"message"` → sends to agent loop.
   - `"tool_response"` → sends to approval broker via a request-ID-keyed channel map.
   - Session CRUD (`new_session`, `load_session`, `delete_session`) → handles directly.
4. **agentLoop**: Runs the LLM loop for the active request. Sends tokens, approval requests, and results to `outbound`.

This topology ensures the WebSocket reader is never blocked by the agent loop or approval waits.

---

## Data Model

### SQLite Schema

```sql
CREATE TABLE sessions (
    id          TEXT PRIMARY KEY,  -- UUID
    title       TEXT NOT NULL DEFAULT 'New Session',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE messages (
    id          TEXT PRIMARY KEY,  -- UUID
    session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    role        TEXT NOT NULL,     -- 'user', 'assistant', 'tool'
    content     TEXT,              -- text content, nullable for tool-call-only messages
    tool_calls  TEXT,              -- JSON array of tool calls (null if none)
    tool_call_id TEXT,             -- set when role='tool', references the originating call
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE tool_results (
    id          TEXT PRIMARY KEY,
    message_id  TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    tool_name   TEXT NOT NULL,
    params      TEXT NOT NULL,     -- JSON
    result      TEXT,              -- output string, null if denied
    approved    BOOLEAN NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_messages_session ON messages(session_id, created_at);
CREATE INDEX idx_tool_results_message ON tool_results(message_id);
```

### SQLite Initialization

After opening the database, the following pragmas are set before any other operations:

```go
func OpenDB(path string) (*sql.DB, error) {
    db, err := sql.Open("sqlite3", path)
    if err != nil {
        return nil, err
    }

    pragmas := []string{
        "PRAGMA journal_mode=WAL",       // concurrent readers during writes
        "PRAGMA busy_timeout=5000",      // retry for 5s instead of immediate SQLITE_BUSY
        "PRAGMA synchronous=NORMAL",     // safe with WAL, better performance
        "PRAGMA foreign_keys=ON",        // required: schema uses ON DELETE CASCADE
    }
    for _, p := range pragmas {
        if _, err := db.Exec(p); err != nil {
            db.Close()
            return nil, fmt.Errorf("setting %s: %w", p, err)
        }
    }

    return db, nil
}
```

### WebSocket Message Protocol

All messages are JSON with a `type` field. The frontend includes its tab UUID in the WS upgrade URL: `ws://host:port/ws?tab=<uuid>`.

**Server → Client:**

```jsonc
// Streamed token
{ "type": "token", "data": "Hello" }

// Tool approval request
{ "type": "tool_approval", "request_id": "uuid", "tool": "shell", "params": { "command": "ls -la" } }

// Tool execution result (after approval, for display)
{ "type": "tool_result", "request_id": "uuid", "tool": "shell", "result": "file1.txt\nfile2.txt", "approved": true }

// Stream complete
{ "type": "done", "message_id": "uuid" }

// Error
{ "type": "error", "message": "LLM endpoint unreachable" }

// Session list update
{ "type": "sessions", "data": [ { "id": "...", "title": "...", "updated_at": "..." } ] }

// Full session history (sent in response to load_session)
{
  "type": "history",
  "session_id": "uuid",
  "messages": [
    {
      "id": "uuid",
      "role": "user",
      "content": "Explain this code",
      "tool_calls": null,
      "tool_call_id": null,
      "tool_results": [],
      "created_at": "ISO8601"
    }
  ]
}
```

The `history` message includes `tool_results` nested inside each message (pre-joined from the `tool_results` table), so the client does not need to perform a join.

**Client → Server:**

```jsonc
// User message
{ "type": "message", "session_id": "uuid", "content": "Explain this code" }

// Tool approval response
{ "type": "tool_response", "request_id": "uuid", "approved": true }

// Create session
{ "type": "new_session" }

// Switch session (load history)
{ "type": "load_session", "session_id": "uuid" }

// Delete session
{ "type": "delete_session", "session_id": "uuid" }
```

---

## Tool Interface

```go
package tools

import (
    "context"
    "encoding/json"
)

// Tool is the interface every tool must implement. One file per tool.
type Tool interface {
    // Name returns the tool name as sent to the LLM (e.g., "shell", "file_read").
    Name() string

    // Description returns a short description for the LLM's tool list.
    Description() string

    // Parameters returns the JSON Schema describing the tool's input parameters.
    Parameters() json.RawMessage

    // Execute runs the tool with the given parameters and returns the output string.
    // Context carries cancellation from session teardown, approval denial, or shutdown.
    Execute(ctx context.Context, params json.RawMessage) (string, error)
}
```

### Registration

```go
package tools

import (
    "fmt"
    "sync"
)

type Registry struct {
    mu    sync.RWMutex
    tools map[string]Tool
}

func NewRegistry() *Registry {
    return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(t Tool) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    if _, exists := r.tools[t.Name()]; exists {
        return fmt.Errorf("tool %q already registered", t.Name())
    }
    r.tools[t.Name()] = t
    return nil
}

func (r *Registry) Deregister(name string) {
    r.mu.Lock()
    defer r.mu.Unlock()
    delete(r.tools, name)
}

func (r *Registry) Get(name string) (Tool, bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    t, ok := r.tools[name]
    return t, ok
}

func (r *Registry) All() []Tool {
    r.mu.RLock()
    defer r.mu.RUnlock()
    out := make([]Tool, 0, len(r.tools))
    for _, t := range r.tools {
        out = append(out, t)
    }
    return out
}
```

Local tools are registered explicitly in `main()` or a setup function at startup — not via `init()`. This makes initialization order deterministic and avoids hidden import side effects.

```go
func registerBuiltinTools(reg *Registry) {
    reg.Register(&ShellTool{})
    reg.Register(&FileReadTool{})
    reg.Register(&FileWriteTool{})
}
```

MCP tools are registered dynamically via `Register()` when discovered, and removed via `Deregister()` on disconnect.

---

## Built-in Tools (Initial Set)

1. **shell** — Executes a shell command. On Unix: `sh -c <command>`. On Windows: `cmd /C <command>`. Returns stdout + stderr. Parameters: `{ "command": "string" }`.
2. **file_read** — Reads a file and returns its content. Parameters: `{ "path": "string" }`.
3. **file_write** — Writes content to a file. Creates parent directories if needed. Parameters: `{ "path": "string", "content": "string" }`.

That's it for v1. More tools are added as new `.go` files.

---

## Approval System

### Rules (evaluated in order)

1. Check the whitelist in config. If the tool name matches an entry with `allow: always`, auto-approve.
2. If the tool name matches an entry with `allow: never`, auto-deny.
3. Otherwise, send an approval request to the frontend and block.

### Blocking Mechanism

The approval broker blocks using a `select` across three channels:

```go
select {
case decision := <-approvalChan:
    return decision, nil
case <-ctx.Done():
    return false, ctx.Err()
case <-time.After(timeout):
    return false, ErrApprovalTimeout
}
```

This ensures the broker unblocks cleanly on shutdown (context cancellation), user timeout, or user decision.

### Whitelist Config

```yaml
approval:
  default_timeout: 300  # seconds, 0 = no timeout
  whitelist:
    - tool: "file_read"
      allow: always
    - tool: "shell"
      allow: ask        # default behavior, explicit for clarity
```

### Always-Allow Mode

A global override `approval.auto_approve_all: true` in config skips all approval prompts. Useful for unattended/scripted usage. Disabled by default.

---

## MCP Client Integration

### Config

```yaml
mcp_servers:
  - name: "filesystem"
    transport: "stdio"
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/home/user/projects"]

  - name: "remote-tools"
    transport: "sse"
    url: "http://192.168.1.50:8081/mcp"
```

### Behavior

- On startup: connect to each configured MCP server sequentially.
- On successful connection: call `initialize`, then `tools/list`.
- For each tool returned: create a wrapper implementing the `Tool` interface, with `Name()` prefixed by the server name (e.g., `filesystem.read_file`) to avoid collisions with local tools.
- On `Register()` collision (two MCP servers expose a tool with the same prefixed name, or it collides with a local tool): skip the conflicting tool, log a warning, continue registering the remaining tools from that server. Do not fail the server connection.
- On `Execute()`: call `tools/call` on the MCP server with the tool name and params, return the result.
- On connection failure at startup: log a warning, skip the server, continue. Do not block startup.
- On mid-session disconnect: de-register tools, log an error, notify the frontend via WebSocket error message.
- No auto-reconnect in v1.

---

## LLM Client: Tool Call Streaming Strategy

When streaming is enabled, tool call arguments arrive as string deltas across multiple SSE chunks. The client handles this as follows:

1. Maintains a per-tool-call string buffer, keyed by tool call index.
2. Appends each `arguments` delta to the buffer.
3. Does **not** attempt to parse the buffer on each chunk.
4. On receiving a chunk with `finish_reason: "tool_calls"` (or `"stop"`), parses each buffered argument string as JSON.
5. If parsing fails at that point, it is a genuine LLM error — surface it to the user as a `"type": "error"` WebSocket message.

This avoids false positives from partial JSON (nested braces, strings containing braces, etc.).

---

## Frontend (Preact + TypeScript)

### Build

- esbuild bundles `web/src/` → `web/dist/`.
- Output: single `index.html`, single `app.js`, single `app.css`.
- No CSS framework. Minimal hand-written CSS. System font stack.
- `web/dist/` is embedded into the Go binary via `//go:embed web/dist/*`.

### Components

1. **App** — Root. Manages WebSocket connection and global state. Generates a tab UUID on mount and includes it in the WS upgrade URL.
2. **Sidebar** — Lists sessions. Click to load. Button to create new. Button to delete.
3. **ChatView** — Scrollable message list. Input box at bottom. Messages render markdown (use a lightweight markdown-to-HTML library or skip and use `white-space: pre-wrap` for v1).
4. **ApprovalModal** — Overlay shown when a `tool_approval` message arrives. Shows tool name, formatted params, and Allow / Deny buttons. Blocks further input until resolved.
5. **ToolResultBlock** — Inline display of tool call + result within the chat, collapsed by default, expandable.

### State Management

- Single state object managed in `App` via `useState`/`useReducer`. No external state library.
- WebSocket messages update state via a dispatch function.
- Session history loaded in full when switching sessions (sent from server as a `history` WebSocket message).

### WebSocket Reconnection

On unexpected `onclose`, the frontend attempts to reconnect with exponential backoff (1s, 2s, 4s, max 30s). The same tab UUID is reused across reconnections. On successful reconnect, the client re-sends `load_session` for the current session to resync state.

---

## WebSocket Keepalive

The server sends a WebSocket Ping frame every 30 seconds. If no Pong is received within 10 seconds, the connection is considered dead and is closed server-side. Browsers handle Pong responses automatically — no custom client-side logic is needed.

```go
const (
    pingInterval = 30 * time.Second
    pongWait     = 40 * time.Second // pingInterval + grace
    writeWait    = 10 * time.Second
)

// In wsReader setup:
conn.SetReadDeadline(time.Now().Add(pongWait))
conn.SetPongHandler(func(string) error {
    conn.SetReadDeadline(time.Now().Add(pongWait))
    return nil
})

// In a separate goroutine:
ticker := time.NewTicker(pingInterval)
defer ticker.Stop()
for {
    select {
    case <-ticker.C:
        conn.SetWriteDeadline(time.Now().Add(writeWait))
        if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
            return // connection dead
        }
    case <-ctx.Done():
        return
    }
}
```

---

## Config File

Full example:

```yaml
# ~/.agent-chat/config.yaml

endpoint:
  url: "http://localhost:8080/v1"
  model: "my-local-model"
  max_tokens: 4096
  temperature: 0.7

server:
  host: "127.0.0.1"  # default: localhost only. Set to 0.0.0.0 for LAN access.
  port: 3000

storage:
  path: "~/.agent-chat/data.db"

approval:
  default_timeout: 300
  auto_approve_all: false
  whitelist:
    - tool: "file_read"
      allow: always
    - tool: "shell"
      allow: ask

mcp_servers: []

# System prompt prepended to every conversation
system_prompt: |
  You are a helpful coding assistant. You have access to tools for
  reading files, writing files, and executing shell commands. Use them
  when needed to help the user.
```

### Path Resolution

All paths in the config (`storage.path`, `--config` flag, `--db` flag) are resolved to absolute paths at parse time. `~` is expanded using `os.UserHomeDir()`. The parsed config struct contains only fully resolved paths.

```go
func expandHome(path string) (string, error) {
    if !strings.HasPrefix(path, "~") {
        return path, nil
    }
    home, err := os.UserHomeDir()
    if err != nil {
        return "", fmt.Errorf("cannot resolve ~: %w", err)
    }
    return filepath.Join(home, path[1:]), nil
}
```

### LAN Access Warning

On startup, if the resolved host is `0.0.0.0` or `::`, the server logs a prominent warning:

```
WARNING: Server is listening on all interfaces with no authentication.
Any device on your network can access this agent and execute tools.
```

---

## CLI Interface

```
Usage: agent-chat [flags]

Flags:
  --config string   Path to config file (default "~/.agent-chat/config.yaml")
  --port int        Override server port
  --host string     Override server host
  --db string       Override database path
  --verbose         Enable debug logging
  --version         Print version and exit
```

No subcommands in v1. The binary does one thing: start the server.

---

## Graceful Shutdown

Shutdown is triggered by SIGINT or SIGTERM and follows this sequence:

```go
func main() {
    ctx, cancel := context.WithCancel(context.Background())

    go func() {
        sig := make(chan os.Signal, 1)
        signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
        <-sig
        cancel()
    }()

    // Pass ctx to server, agent loop, approval broker, MCP clients...
}
```

### Shutdown Sequence

1. **Cancel root context.** All goroutines that select on `ctx.Done()` begin winding down.
2. **Stop accepting new WebSocket connections.** `http.Server.Shutdown()` with a 10-second deadline.
3. **Unblock active agent loops.** Context cancellation unblocks any pending approval waits (via the `select` in the approval broker) and in-flight LLM HTTP requests.
4. **Close MCP subprocess connections.** Send SIGTERM to each stdio subprocess, wait up to 5 seconds, then SIGKILL. Close SSE connections.
5. **Close SQLite database.** `db.Close()` after all goroutines using it have exited.
6. **Exit.**

---

## Build & Development

### Prerequisites

- Go 1.22+
- Node.js 18+ (for frontend build only, not at runtime)

### Commands

```bash
# Install frontend dependencies
cd web && npm install

# Build frontend
cd web && npx esbuild src/app.tsx --bundle --outdir=dist --minify

# Build Go binary (embeds frontend)
go build -o agent-chat ./cmd/agent

# Run
./agent-chat --config ./config.yaml
```

### Makefile Targets

```makefile
.PHONY: frontend build run clean

frontend:
	cd web && npx esbuild src/app.tsx --bundle --outdir=dist --minify --loader:.css=css

build: frontend
	go build -o agent-chat ./cmd/agent

run: build
	./agent-chat

clean:
	rm -rf agent-chat web/dist web/node_modules
```

---

## What Is Explicitly Out of Scope for v1

- Authentication / multi-user support
- HTTPS / TLS (use a reverse proxy if needed)
- Auto-reconnect for MCP servers
- Conversation branching or forking
- Image or multi-modal support
- Rate limiting
- Tool output streaming (tool output returned as a complete string)
- Mobile-optimized UI
- Automated testing (add once the core stabilizes)
- Hot-reloading config

---

## Dependencies (Go)

- `github.com/mattn/go-sqlite3` — SQLite driver (CGO) or `modernc.org/sqlite` (pure Go, preferred for cross-compilation)
- `github.com/gorilla/websocket` — WebSocket implementation (or `nhooyr.io/websocket` for a more modern API)
- `gopkg.in/yaml.v3` — YAML config parsing
- `github.com/google/uuid` — UUID generation
- Standard library for everything else (net/http, encoding/json, os/exec, io, context)

No web framework. `net/http` with a small hand-rolled router (or `http.NewServeMux` from Go 1.22 with method+pattern support) is sufficient.

## Dependencies (Frontend)

- `preact` — UI rendering
- `htm` — Tagged template alternative to JSX if avoiding a JSX transform, otherwise use esbuild's JSX support with Preact pragma
- No other runtime dependencies in v1
