# Non-Interactive CLI Mode

## Summary

Add a `--prompt` flag that runs gohome in headless (non-interactive) mode. The agent receives the prompt, executes with full tool access (requires `--yolo`), and prints the final response to stdout. Optionally, `--verbose` emits all events as JSON lines.

## CLI Interface

New flags:
- `--prompt <string>` — triggers non-interactive mode.
- `--verbose` — emit events as JSONL to stdout (only valid with `--prompt`).

Constraints:
- `--prompt` requires `--yolo`. Exit 1 with error if missing.
- `--verbose` without `--prompt` is an error.
- Empty `--prompt ""` is an error.

Exit codes:
- `0` — agent completed successfully.
- `1` — any error (config, LLM, agent, flag validation).

## Architecture

### New package: `internal/headless`

File: `gohome/internal/headless/frontend.go`

```go
type Frontend struct {
    prompt  string
    verbose bool
    output  io.Writer   // stdout, injected for testability
    sent    bool
    buf     strings.Builder
    mu      sync.Mutex
}
```

Implements `agent.Frontend`:

- `AwaitUserInput(ctx)` — First call returns `f.prompt`. Subsequent calls block until ctx cancelled.
- `RequestApproval(ctx, req)` — Returns `AllowOnce` (should never be called with `--yolo`).
- `Emit(sessionID, ev)`:
  - Plain mode: accumulates `EventTokenDelta` text into `buf`.
  - Verbose mode: JSON-marshals each event and writes as a line to `output`.

Public method:
- `FinalText() string` — returns accumulated buffer contents for main.go to print.

### Changes to `main.go`

Shared setup (unchanged): home/cwd resolution, logging, config, model, LLM client, whitelist, tools registry, session, writer, agent construction.

Branch after agent construction:

**Headless path (`--prompt` set):**
1. Validate `--yolo` is set.
2. Create `headless.NewFrontend(prompt, verbose, os.Stdout)`.
3. Wire into guard (same as TUI path).
4. Inject prompt as first user message into `sess.History` and persist via writer.
5. Call `a.Run(ctx, sess)` directly.
6. On success: print `fe.FinalText()` to stdout (plain mode only).
7. Emit session_end, close writer, close log, exit.

**TUI path (`--prompt` not set):** existing code unchanged.

## Output Behavior

### Plain text (default)

Only the final assistant text response is printed to stdout with a trailing newline. Tool calls, thinking, and other events are silent.

### Verbose (--verbose)

Every event is written as a JSON line to stdout as it arrives. Each line is a JSON object with the event fields from `agent.Event`. The final text is embedded in the stream as `token_delta` events.

## Session Persistence

Session JSONL is written identically to interactive mode. Non-interactive sessions can be resumed later with `--resume`.

## Testing

Unit tests (`internal/headless/frontend_test.go`):
- `TestAwaitUserInput_FirstCall` — returns prompt.
- `TestAwaitUserInput_SecondCall_Blocks` — blocks until context cancelled.
- `TestEmit_PlainText` — accumulates token deltas correctly.
- `TestEmit_Verbose` — writes JSON lines.
- `TestRequestApproval_AllowsAlways` — returns AllowOnce.

Integration test:
- Wire headless frontend to fake LLM client, verify end-to-end flow.

Edge case tests:
- `--prompt` without `--yolo` exits with error.
- `--verbose` without `--prompt` exits with error.
- Empty prompt exits with error.
