# Design: Remove Daemon Mode, Restore In-Process Architecture

**Date:** 2026-07-10
**Target version:** v0.4.0

## Context

Daemon mode was introduced in v0.3.0 (PR #20) adding ~3,085 LOC of JSON-RPC infrastructure for agent-TUI communication over a Unix socket. Post-release, 8+ critical bugs were fixed (concurrency races, channel stalls, goroutine leaks, Ctrl+C hangs), all caused by the daemon architecture. The feature broke Windows support entirely (Unix sockets) and the stated benefits (multi-client support, TUI reconnection) are not needed for this project's use case: a simple, personal CLI coding agent.

All user-facing features added in v0.3.x (model switching, visual overhaul, config overlay, reasoning_effort) can be implemented as in-process changes without daemon infrastructure.

## Decision

Revert to the v0.2.5 in-process architecture and port forward all non-daemon improvements from v0.3.x.

## Strategy

**Approach:** Branch from `v0.2.5` tag, port forward non-daemon improvements.

Starting from v0.2.5 is cleaner than stripping daemon code from HEAD because:
- v0.2.5's main.go, tui/frontend.go, and agent wiring are known-good
- No risk of missing a daemon dependency
- Forward-porting rendering changes is straightforward since they don't depend on daemon

## What gets removed (by starting from v0.2.5)

- `internal/daemon/` -- server, RPC protocol, frontend, dispatch, handlers, loop (3,085 LOC)
- `tui/client_frontend.go` -- TUI-side RPC client (298 LOC)
- Mutex machinery on Guard.frontend and Agent.Frontend
- Socket lifecycle management in main.go
- `--stop` CLI flag
- Unix socket dependency (restores Windows support)

## What gets ported forward

### A. Visual overhaul (PRs #21, #23)

Rendering-only changes, no daemon dependency:
- Contextual tool call headers ($ cmd, file paths instead of [tool])
- User message blocks with styled left border
- Tool call visual grouping with color-coded borders
- Per-tool execution timing
- Per-response performance stats (tokens/sec, cache hits)
- Status bar: project directory + git branch
- Scroll improvements: gradient fades, scroll position, line-by-line arrows
- Expand hint on truncated tool output
- Markdown styling (cyan headings, OSC 8 hyperlinks)
- Uniform entry spacing
- Streamlined approval overlay
- Thinking content always inline as dim italic

### B. Config improvements (PR #24)

- /config overlay with source annotations
- Setup wizard for first-run settings creation
- --config CLI flag
- reasoningEffort model config field (rewired to in-process agent params)

### C. In-process model switching (replaces daemon RPC)

/model slash command rebuilds the LLM client directly via callback in main.go:
1. Look up model config from settings
2. Create new LLM client via llm.New()
3. Call state.SetClient(newClient)
4. Update agent maxTokens, thinkingBudget, reasoningEffort
5. Show config name in status bar

### D. Small fixes

- filepath.Dir replacing custom dirOf
- parseListing rejecting empty session IDs
- Whitespace-only LLM text skipped
- Session focus keys rebound to Ctrl+Left/Right
- OpenAI reasoning_effort wire protocol support

## Target architecture

Single process, single agent goroutine, direct channel communication:

```
gohome process
+-- main()
|   +-- Load config, create LLM client
|   +-- Create Guard with Frontend (set once, no mutex)
|   +-- Create TUI Frontend (channels to Bubble Tea)
|   +-- Build tools, load session
|   +-- Start Bubble Tea + agent goroutine
|
+-- Agent goroutine
|   +-- AwaitUserInput (blocks on channel)
|   +-- agent.Run (Turn, tool dispatch)
|   +-- Guard.Check -> Frontend.RequestApproval (direct call)
|   +-- Frontend.Emit (direct Bubble Tea Send)
|
+-- TUI (Bubble Tea main thread)
    +-- Receives events from Frontend.Emit
    +-- Slash commands call callbacks directly
    +-- User input -> inputCh -> agent goroutine
```

No socket, no RPC, no daemon process. Frontend set once at startup. Works on all platforms.

## Implementation phases

### Phase 1: Create branch, verify base
Branch from v0.2.5, confirm build and tests pass.

### Phase 2: Port visual overhaul (PRs #21, #23)
Diff TUI files between v0.2.5 and HEAD, apply rendering-only hunks. Key files: chat.go, statusbar.go, model.go, model_keys.go, approval.go, markdown.go, help.go. Regenerate snapshots.

### Phase 3: Port config improvements (PR #24)
Copy skeleton.go, config_overlay.go, config_wizard.go, config_editor.go. Port config.go changes. Wire reasoningEffort directly. Add --config flag.

### Phase 4: In-process model switching
Port /model callback to directly rebuild LLM client and update agent params. Port config name display in status bar.

### Phase 5: Small fixes and cleanup
Port remaining fixes, update go.mod, update README/CHANGELOG, remove daemon references.

### Phase 6: Verify and ship
Full test suite, go vet, golangci-lint, manual testing, Windows cross-build verification.

## CLI flags (v0.4.0)

| Flag | Description |
|------|-------------|
| --model | Select a configured model config by name |
| --yolo | Start with all approval prompts disabled |
| --resume | Resume most recent session for current directory |
| --config | Print merged configuration and exit |
| --version | Print version and exit |

## Risk assessment

**Low risk:** Visual/rendering changes are isolated to TUI rendering functions.
**Low risk:** Config changes are self-contained in config package + new TUI overlay files.
**Medium risk:** Model switching rewiring requires careful callback plumbing in main.go, but v0.2.5 already had the /model command infrastructure.
**Low risk:** Windows support restored by removing Unix socket dependency.
