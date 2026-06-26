# Daemon Mode Design

## Summary

Replace the current single-process model (agent + TUI in one binary) with a
daemon-only architecture. The agent runs as a background daemon process
communicating with a Bubble Tea TUI client over JSON-RPC 2.0 on a Unix socket.
Subagents remain goroutines within the daemon but emit events over the same
JSON-RPC channel, simplifying the TUI's Frontend coupling.

## Goals

1. **Daemon-only mode:** `gohome` always runs as daemon + client. No embedded
   fallback.
2. **Persistent agent:** TUI can disconnect and reconnect without killing the
   agent or losing state.
3. **Subagent simplification:** Decouple child agents from the TUI's
   `tea.Program`. The TUI receives all events (parent and child) as JSON-RPC
   notifications routed by `sessionID`.
4. **Preserve subagent UI:** Tabs, shadow entries, and child session timelines
   remain.

## Process Model and Lifecycle

### Startup

1. User runs `gohome` (or `gohome --model <name>`).
2. Binary checks for `~/.gohome/daemon.sock`.
   - Socket exists and responds to `daemon.health`: connect as TUI client.
   - Socket exists but dead: remove stale file, start daemon.
   - No socket: start daemon.
3. Starting the daemon: fork into background, then connect as TUI client. From
   the user's perspective, one command, TUI appears.

### Daemon lifecycle

- Listens on `~/.gohome/daemon.sock`.
- Owns agent, tools, guard, LLM clients, session state, and JSONL writers.
- Accepts one TUI client at a time (multi-client is future work).
- On TUI disconnect: stays alive if a turn is in-flight. If idle, exits after
  a configurable grace period (default 30s).
- `gohome --stop` sends a `daemon.stop` RPC and waits for clean exit.

### Shutdown ordering

1. TUI sends `daemon.stop` or disconnects.
2. Daemon cancels in-flight agent turns, waits for goroutines.
3. Emits `session_end`, closes JSONL writers, removes socket, exits.

## JSON-RPC Protocol

JSON-RPC 2.0 over Unix socket. Daemon is server, TUI is client.

### Daemon -> TUI (Notifications)

| Method | Params | Description |
|--------|--------|-------------|
| `agent.event` | `{sessionID, event}` | All agent events: token deltas, tool calls, results, session started/ended, usage, errors, thinking. |
| `session.state` | `{sessionID, model, yolo, timeline, pendingApproval?}` | Sent on TUI connect for state reconstruction. |

### TUI -> Daemon (Requests)

| Method | Params | Result | Description |
|--------|--------|--------|-------------|
| `session.input` | `{sessionID, text}` | `{}` | User submits a message. |
| `session.approval` | `{sessionID, decision}` | `{}` | User responds to approval prompt. |
| `session.new` | `{}` | `{sessionID}` | Create a new session. |
| `session.resume` | `{id}` | `{sessionID, history}` | Resume a past session. |
| `session.list` | `{}` | `{sessions[]}` | List available sessions. |
| `session.cancel` | `{sessionID}` | `{}` | Cancel current turn. |
| `model.set` | `{name}` | `{modelName, contextWindow}` | Switch model. |
| `daemon.health` | `{}` | `{version, uptime}` | Health check. |
| `daemon.stop` | `{}` | `{}` | Request clean shutdown. |

### Daemon -> TUI (Requests)

| Method | Params | Result | Description |
|--------|--------|--------|-------------|
| `approval.request` | `{sessionID, tool, input, summary, suggestedPattern}` | `{decision}` | Daemon asks TUI for approval. Blocks the agent goroutine until TUI responds. |

## Package Structure

### New packages

| Package | Purpose |
|---------|---------|
| `internal/daemon` | Daemon server: Unix socket listener, JSON-RPC dispatch, agent lifecycle. |
| `internal/daemon/rpc` | JSON-RPC 2.0 message types, codec, transport helpers. |
| `internal/daemon/frontend` | `RPCFrontend` implements `agent.Frontend` by serializing Emit/RequestApproval/AwaitUserInput over JSON-RPC. |

### Packages that change

| Package | Change |
|---------|--------|
| `cmd/gohome/main.go` | Rewritten: detect daemon, connect or start, run TUI client loop. |
| `internal/tui/frontend.go` | No longer implements `agent.Frontend`. Receives JSON-RPC notifications from daemon and translates to `AgentEventMsg` for Bubble Tea. Sends user input/approvals as JSON-RPC requests. |
| `internal/agent/spawn.go` | Child agent gets its own `RPCFrontend` tagged with `childID`. No shared `tea.Program` reference. |

### Packages unchanged

`internal/agent` (Run, Turn, events, state), `internal/tools`,
`internal/guard`, `internal/llm`, `internal/session`,
`internal/tui` (model, rendering, overlays, chat, editor).

## Data Flow

### Normal turn

```
User types -> TUI sends "session.input" -> Daemon receives, appends to history
  -> Agent.Run -> Agent.Turn -> LLM stream
  -> RPCFrontend.Emit serializes events
  -> Daemon sends "agent.event" notifications
  -> TUI receives, sends AgentEventMsg to Bubble Tea
  -> handleAgentEvent routes by sessionID + EventKind
```

### Subagent turn

```
LLM requests subagent tool -> dispatchTool -> Spawn
  -> Child agent created with own RPCFrontend (tagged childID)
  -> Child runs in new goroutine, parent goroutine blocks on Spawn
  -> RPCFrontend.Emit(childID, event) -> daemon event bus
  -> Daemon sends "agent.event" with sessionID=childID
  -> TUI routes to child SessionView
  -> Child finishes, Spawn returns result to parent
```

Parent goroutine blocks on Spawn (awaiting child result for tool response).
This is correct: the LLM expects a tool result. The TUI keeps receiving events
from both sessions because events flow over JSON-RPC, not through the blocked
goroutine.

### Approval flow

```
Guard.Check -> not whitelisted
  -> RPCFrontend.RequestApproval sends "approval.request" to TUI
  -> TUI shows overlay, user decides
  -> TUI sends JSON-RPC response with decision
  -> RPCFrontend.RequestApproval unblocks, returns decision to guard
```

## Error Handling

- **Daemon crash:** TUI detects broken socket, shows error, exits.
- **TUI crash/disconnect:** Daemon stays alive. Pending approvals auto-deny
  after timeout (default 30s). Queued until new TUI connects.
- **LLM errors:** Unchanged. EventError flows through JSON-RPC to TUI.
- **Socket cleanup:** Daemon removes `daemon.sock` on clean exit. Stale sockets
  (connection refused) are auto-removed on startup.

## Reconnection

When a new TUI connects to a running daemon:

1. Daemon sends `session.state` with current session ID, model, YOLO mode, and
   full timeline.
2. Pending approval requests are re-sent.
3. Streaming resumes.

## Testing Strategy

- **Unit tests:** `RPCFrontend` serialization/deserialization using `net.Pipe()`.
- **Integration tests:** In-process daemon + test client over `net.Pipe()` or
  localhost TCP.
- **Existing tests:** TUI snapshot tests use `AgentEventMsg` directly (transport
  layer is below them). Agent tests use mock `Frontend`. Both unchanged.

## Subagent Simplification Summary

The key change: `tui.Frontend` no longer needs to be thread-safe for concurrent
calls from parent and child agents. Today `Frontend.Emit` is called from both
parent and child goroutines sharing the same `tea.Program` reference. With
daemon mode, each agent (parent or child) has its own `RPCFrontend` that
serializes events into the daemon's JSON-RPC stream. The TUI receives events on
a single reader goroutine. `handleAgentEvent`, shadow entries, and
`childToParent` tracking stay, but their inputs are cleaner.
