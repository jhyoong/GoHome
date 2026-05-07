# agent-chat

A single-binary Go server that runs a local AI chat interface in your browser. It connects to any OpenAI-compatible LLM endpoint, executes tools (shell, file read/write) with your approval, and persists sessions in SQLite.

## Requirements

- Go 1.22+
- A local OpenAI-compatible LLM endpoint (e.g. llama.cpp, Ollama)

## Quick Start

```bash
# Build frontend and binary
make build

# Run with defaults (connects to http://localhost:8080/v1)
./agent-chat

# Open http://localhost:3000 in your browser
```

## Configuration

Create `~/.agent-chat/config.yaml`:

```yaml
endpoint:
  url: "http://localhost:8080/v1"
  model: "my-model"
  max_tokens: 4096
  temperature: 0.7

server:
  host: "127.0.0.1"
  port: 3000

storage:
  path: "~/.agent-chat/data.db"

system_prompt: "You are a helpful assistant."

approval:
  default_timeout: 300   # seconds to wait for user approval
  auto_approve_all: false
  whitelist:
    - tool: "file_read"
      allow: "always"    # always | never | ask

mcp_servers:
  - name: "my-server"
    transport: "stdio"   # stdio | sse
    command: "my-mcp-server"
    args: ["--flag"]
```

All fields are optional — defaults are used if the config file is missing.

## CLI Flags

```
--config   Path to config file (default: ~/.agent-chat/config.yaml)
--port     Override server port
--host     Override server host
--db       Override database path
--verbose  Enable debug logging
--version  Print version and exit
```

## Make Targets

```
make build     Build binary
make test      Run all tests
make run       Build and run
make clean     Remove build artifacts
```

## Architecture

```
cmd/agent/          Binary entry point
embed.go            Embeds web/static into the binary
internal/
  config/           YAML config loader with tilde expansion
  session/          SQLite store (sessions, messages, tool results)
  tools/            Tool interface, registry, shell/file_read/file_write
  approval/         Per-connection approval broker (whitelist, timeout)
  llm/              OpenAI-compatible client (streaming SSE)
  mcp/              MCP client (stdio and SSE transports)
  agent/            Agentic loop — streams LLM, executes tools, loops back
  server/           HTTP REST + WebSocket (4-goroutine model per connection)
web/static/         Vanilla JS frontend (no build step)
```

Each WebSocket connection runs 4 goroutines: reader, writer, pingLoop, dispatcher. The agent loop streams tokens to the browser and pauses for user approval before executing any tool.

## Tests

Run all tests:

```bash
go test ./...
```

| Package | Tests |
|---------|-------|
| `internal/config` | `TestParseConfig` — YAML parsing, tilde expansion, defaults |
| `internal/session` | `TestOpenAndMigrate`, `TestSessionCRUD`, `TestMessageCRUD`, `TestToolResultCRUD` |
| `internal/tools` | `TestRegistry`, `TestToLLMTools`, `TestShellTool`, `TestFileReadTool`, `TestFileWriteTool` |
| `internal/approval` | `TestAutoApproveWhitelist`, `TestAutoDenyWhitelist`, `TestAutoApproveAll`, `TestApprovalTimeout`, `TestApprovalContextCancel`, `TestApprovalUserDecision` |
| `internal/llm` | `TestNonStreamingComplete`, `TestStreamingTokens` |
| `internal/agent` | `TestSimpleMessageRoundtrip` — full loop with mock LLM, verifies streaming and persistence |
| `internal/server` | `TestListSessions`, `TestCreateSession` |
