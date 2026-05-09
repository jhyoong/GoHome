# Always Allow Design

## Goal

Add an "Always Allow" button to the tool approval modal. When clicked, the tool (or shell command pattern) is added to the whitelist in memory and persisted to the config file, so future calls are auto-approved without prompting.

## Behaviour Summary

- **Non-shell tools**: "Always Allow" matches by tool name only. One click, no extra input.
- **Shell tool, simple command**: "Always Allow" shows an editable pattern input pre-filled with `<base_command> *`. User can make it more specific (e.g. `ls -la *`) before confirming.
- **Shell tool, chained/piped command**: "Always Allow" button is hidden. Only Allow and Deny are shown. The broker also enforces this server-side — a whitelist entry for shell can never auto-approve a chained command.

## Config Schema

`WhitelistEntry` gains one optional field:

```yaml
approval:
  whitelist:
    - tool: file_read
      allow: always
    - tool: shell
      allow: always
      command_pattern: "ls *"
```

`command_pattern` is only used when `tool == "shell"`. It is ignored for all other tools.

A new `config.Save(path string, cfg *Config) error` function rewrites the full YAML config file (same `~` expansion as `Load`).

## WebSocket Protocol

New inbound message type `always_allow`:

```json
{ "type": "always_allow", "request_id": "abc", "tool": "file_read" }
{ "type": "always_allow", "request_id": "abc", "tool": "shell", "command_pattern": "ls *" }
```

Handling this message also implicitly approves the current pending request. No separate `tool_response` is needed.

## Approval Broker

- `AddWhitelistEntry(entry config.WhitelistEntry)` — thread-safe in-memory update.
- In `Request`, for `tool == "shell"`:
  1. Parse `command` from `params` JSON.
  2. If the command contains `&&`, `||`, `;`, or `|` outside of quotes → skip whitelist, fall through to approval.
  3. Otherwise check `entry.CommandPattern` with a custom glob matcher where `*` matches any sequence of characters including spaces and slashes.
- The chain/pipe check always runs before any whitelist lookup, preventing patterns like `ls *` from auto-approving `ls /tmp && rm -rf /`.

## Server

`server.Config` gains two fields:

```go
type Config struct {
    Store      *session.Store
    Loop       *agent.Loop
    Approval   config.ApprovalConfig  // unchanged, used for broker construction
    FullConfig *config.Config         // nil if no config file on disk
    ConfigPath string                 // e.g. "~/.agent-chat/config.yaml"
}
```

`Server` gains `approvalMu sync.RWMutex` to protect concurrent whitelist updates across connections.

`always_allow` handler (in dispatcher):
1. `broker.AddWhitelistEntry(entry)` — current connection takes effect immediately.
2. Under write lock: append entry to `s.cfg.Approval.Whitelist` so future connections pick it up.
3. If `s.cfg.FullConfig != nil`: mirror update and call `config.Save`.
4. `broker.Respond(msg.RequestID, true)` — approves the waiting call.

`handleWebSocket` reads `s.cfg.Approval` under the read lock when constructing each broker.

`main.go` passes `FullConfig: cfg` and `ConfigPath: *configPath` to the server.

## Frontend

Approval modal changes:
- Add "Always Allow" button alongside Allow and Deny.
- For shell commands: run a JS chain/pipe detector before rendering. If chained → hide "Always Allow", show Allow and Deny only.
- For shell, simple command: clicking "Always Allow" expands the modal card inline to show a text input pre-filled with `<base_command> *` plus Confirm and Cancel buttons.
- For non-shell: clicking "Always Allow" sends the message immediately.

Pattern suggestion rule: take the first whitespace-delimited token of the command, append ` *`.

## Data Flow

```
User clicks "Always Allow"
  → [shell?] Show inline pattern editor pre-filled with "<cmd> *"
  → User edits if needed, clicks Confirm
  → WS: { type: "always_allow", request_id, tool, command_pattern? }
  → Server: AddWhitelistEntry + update s.cfg.Approval + config.Save + broker.Respond(true)
  → Current call approved, future matching calls auto-approved
```
