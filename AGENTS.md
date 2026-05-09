# GoHome Agent Guide

## Build & Test Commands

```bash
make build    # Build binary: go build -o gohome ./cmd/agent
make test     # Run all tests: go test ./...
make run      # Build and run
make clean    # Remove gohome binary
```

CI also runs: `go vet ./...` and `staticcheck ./...` (staticcheck must be installed separately via `go install honnef.co/go/tools/cmd/staticcheck@latest`).

Run a single package:
```bash
go test ./internal/tools
```

## Entry Point

- **Binary**: `cmd/agent/main.go`
- **Web assets**: embedded via `embed.go` (web/static/*). No frontend build step.
- **Config default path**: `~/.gohome/config.yaml` (~ expansion handled in `internal/config`)
- **Database default path**: `~/.gohome/data.db`

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

## Key Operational Notes

- **Config tilde expansion**: paths starting with `~` are expanded to `$HOME/...` in config loader (`internal/config/expandHome`). This applies to both config file path and storage path.
- **Approval whitelist persistence**: when a user clicks "always allow" for a tool/pattern, the server updates the config file on disk (`~/.gohome/config.yaml`) and reloads it on next start.
- **Default endpoint**: `http://localhost:8080/v1` if not configured.
- **Server bind warning**: if `server.host` is `0.0.0.0` or `::`, server logs a warning about no authentication.

## MCP Servers

Configured via `mcp_servers` in config.yaml. Each server registers its tools with the tool registry. Supports two transports:
- `stdio`: spawns command, pipes stdin/stdout
- `sse`: HTTP POST to server URL

Tools from MCP servers are named `serverName.toolName`.

## Release Process

Release is triggered by pushing tags matching `v*`. Uses goreleaser with:
- `goreleaser.yaml` hook: `go mod tidy` before build
- CGO_ENABLED=0 (static binary)
- Multi-platform: linux amd64/arm64, darwin amd64/arm64, windows
- Docker images published to ghcr.io/jhyoong/gohome

Changelog is generated from commit messages grouped by prefix: `feat`, `fix`, other.

## Linting

No `.golangci.yml` config file. CI runs `go vet` and `staticcheck`. You should install staticcheck separately for local pre-release checks.

## Dependencies

- Go 1.25.6
- `modernc.org/sqlite` (pure-Go SQLite)
- `gorilla/websocket` (WebSocket)
- `gopkg.in/yaml.v3` (config parsing)
- `google/uuid`