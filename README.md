# GoHome

A single-binary Go server that runs a local AI chat interface in your browser. It connects to any OpenAI-compatible LLM endpoint, executes tools (shell, file read/write) with your approval, and persists sessions in SQLite.

## Requirements

- Go 1.22+
- A local OpenAI-compatible LLM endpoint (e.g. llama.cpp, Ollama)

## Quick Start

```bash
# Build binary
make build

# Run with defaults (connects to http://localhost:8080/v1)
./gohome

# Open http://localhost:3000 in your browser
```

## Configuration

Create `~/.gohome/config.yaml`:

```yaml
endpoint:
  url: "http://localhost:8080/v1"
  api_key: ""              # optional API key
  model: "my-model"
  max_tokens: 4096
  temperature: 0.7
  context_window: 131072   # context window size (default: 131072)
  thinking_tokens: 0       # thinking tokens for o1 models (default: 0)

server:
  host: "127.0.0.1"
  port: 3000

storage:
  path: "~/.gohome/data.db"

system_prompt: "You are a helpful assistant."

approval:
  default_timeout: 300   # seconds to wait for user approval
  auto_approve_all: false
  whitelist:
    - tool: "file_read"
      allow: "always"    # always | never | ask
    - tool: "shell"
      command_pattern: "ls*"  # optional command pattern for shell tools

mcp_servers:
  - name: "my-server"
    transport: "stdio"   # stdio | sse
    command: "my-mcp-server"
    args: ["--flag"]
    url: ""              # optional URL for SSE transport
```

All fields are optional — defaults are used if the config file is missing.

## CLI Flags

```
--config   Path to config file (default: ~/.gohome/config.yaml)
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