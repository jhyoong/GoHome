# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is gohome

A single-binary terminal coding agent written in Go. It targets custom LLM endpoints (Anthropic and OpenAI wire formats) and provides tool-call guardrails with human approval, plus built-in sequential subagents. The TUI is built with Bubble Tea (charmbracelet/bubbletea).

## Build and run

```sh
# Build (the -o flag is required because the source dir is also named "gohome")
go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome

# Run
./bin/gohome --endpoint <name>
```

## Test commands

```sh
# All unit tests
go test ./gohome/...

# Single package
go test ./gohome/internal/tui/

# Single test
go test ./gohome/internal/tui/ -run TestSnapshots

# Update golden snapshot files
go test ./gohome/internal/tui/ -run TestSnapshots -update

# E2E tests (requires live LLM endpoint, never runs in CI)
go test -tags e2e ./gohome/test/e2e/ \
  -run TestE2ESmokeRoundtrip

# Lint
golangci-lint run ./gohome/...

# Vet
go vet ./gohome/...
```

## Architecture

All source lives under `gohome/`. Entry point: `gohome/cmd/gohome/main.go`.

### Key packages and their roles

- **`internal/agent`** -- Drives the agentic loop. `Agent.Run()` repeatedly calls `Turn()` (one LLM request), dispatches tool calls through the guard, and feeds results back. Owns the `Frontend` interface that the TUI implements. Spawns child subagents via `Spawn()`.
- **`internal/tui`** -- Bubble Tea TUI. `Model` is the root `tea.Model`. Receives events from the agent via `agentEventMsg` and renders timeline entries (user, assistant, thinking, tool, notice). Handles approval prompts, slash commands, file search, session browsing, and overlays.
- **`internal/tools`** -- Tool interface and registry. Each tool (bash, read, write, edit, subagent, session) implements `tools.Tool`. The registry provides JSON schemas to the LLM and dispatches calls.
- **`internal/guard`** -- Whitelist-based approval system. `Guard.Check()` matches tool calls against compiled whitelist rules; if no rule matches, it calls `Frontend.RequestApproval()` to pause for user input. Supports yolo mode (skip all prompts).
- **`internal/llm`** -- LLM client factory. `llm.New()` returns a `common.Client` for either Anthropic or OpenAI wire format. Each wire adapter lives in its own subpackage (`anthropic/`, `openai/`).
- **`internal/config`** -- Settings loading and merging. Global (`~/.gohome/settings.json`) is merged with project-level (`./.gohome/settings.json`). Defaults live in `defaults.go`.
- **`internal/session`** -- Session persistence as JSONL files. Handles session creation, listing, loading (replay from JSONL), and writing events.

### Key interfaces

- `agent.Frontend` -- the boundary between agent and TUI. The TUI implements `Emit`, `RequestApproval`, and `AwaitUserInput`. The agent package never imports `tui`.
- `common.Client` -- the LLM wire abstraction. Implemented by `anthropic.Client` and `openai.Client`.
- `tools.Tool` -- all tools implement `Name()`, `Description()`, `InputSchema()`, `Execute()`.

### Data flow

1. User types in the TUI editor, text goes through `Frontend.input` channel to the agent goroutine in main.go.
2. Agent calls `Turn()` which streams tokens via `Frontend.Emit()`. The TUI receives these as `agentEventMsg` and appends to the session's `Timeline`.
3. When the LLM returns tool_use blocks, `Agent.Run` calls `Guard.Check()` for each. If not whitelisted, the guard calls `Frontend.RequestApproval()` which blocks until the TUI resolves the approval prompt.
4. Tool results feed back into `sess.History` and the loop continues until the LLM stops requesting tools.

### TUI snapshot tests

Golden-file tests in `tui_snapshot_test.go` use `charmbracelet/x/exp/golden`. All state transitions are synchronous through `Model.Update` -- no goroutines or teatest. Color profile is forced to ASCII in `tui_test_main_test.go` for deterministic output. Run with `-update` to regenerate.

## CI

GitHub Actions workflow (`.github/workflows/gohome-ci.yml`): runs `go vet`, `golangci-lint`, and `go test` on ubuntu/macos/windows. Cross-builds for linux/amd64, darwin/arm64, darwin/amd64, windows/amd64 with a 25 MB binary size guard.

## Go version

The project uses Go 1.25 (set in `go.mod`).

## Configuration

Settings file: `~/.gohome/settings.json` (global), `./.gohome/settings.json` (project override). Whitelist files follow the same pattern. See README.md for the full schema.

## Debug logging

Set `GOHOME_DEBUG=1` to enable debug-level structured JSON logging to `~/.gohome/logs/<YYYY-MM-DD>.log`.
