# Interactive CLI Mode Design

## Problem

GoHome's `--prompt` flag is one-shot: it sends a single message, runs the agent loop, and exits. Other CLI tools (e.g. Claude Code) cannot resume or extend a session programmatically. There is no way to have a multi-turn conversation over the headless path.

## Solution

Extend the headless path to support multi-turn interactive sessions via JSONL over stdin/stdout. When `--prompt -` is passed (dash = read from stdin), GoHome reads structured JSON messages from stdin and emits structured JSON events on stdout, including a new `turn_done` event that signals when the agent is ready for the next input.

## CLI Interface

- `--prompt -` activates interactive mode (read JSONL from stdin). Requires `--yolo` and `--verbose`.
- `--prompt "text"` retains current one-shot behavior with no changes.
- `--resume` is compatible with `--prompt -` to load prior session history.

Example invocations:

```sh
# One-shot (unchanged)
gohome --prompt "fix the tests" --yolo

# Interactive, fresh session
gohome --prompt - --yolo --verbose

# Interactive, resume last session
gohome --prompt - --yolo --verbose --resume
```

## Input Protocol (stdin)

One JSON object per line. Two message types:

```jsonl
{"type": "user_message", "content": "fix the failing tests in auth.go"}
{"type": "exit"}
```

- `user_message` -- content string becomes the next user turn.
- `exit` -- graceful shutdown. GoHome emits `session_end` and exits 0.
- EOF on stdin is treated the same as `exit`.
- Malformed JSON lines are skipped with a `warning` event emitted on stdout.

## Output Protocol (stdout)

Existing `--verbose` JSONL events stay as-is. Two new event types:

**`turn_done`** -- emitted after the agent finishes processing each user message:
```json
{"type": "turn_done", "ts": "2026-07-24T10:00:00Z", "sessionId": "abc12345"}
```

**`warning`** -- for non-fatal issues (malformed input, etc.):
```json
{"type": "warning", "ts": "...", "message": "invalid input: unexpected EOF"}
```

### Event flow for one turn

```
caller stdin  ->  {"type": "user_message", "content": "fix auth.go"}
gohome stdout <-  {"type": "assistant_message", ...}
gohome stdout <-  {"type": "tool_result", ...}
gohome stdout <-  {"type": "assistant_message", ...}
gohome stdout <-  {"type": "turn_done", "ts": "...", "sessionId": "abc12345"}
caller stdin  ->  {"type": "user_message", "content": "now run the tests"}
```

### Session end flow

```
caller stdin  ->  {"type": "exit"}
gohome stdout <-  {"type": "session_end", "ts": "...", "reason": "exit"}
(process exits 0)
```

## Implementation Changes

### headless/frontend.go

Extend `AwaitUserInput` to read JSONL from stdin when in interactive mode. Add a `scanner *bufio.Scanner` field (nil for one-shot, set for interactive). On `user_message`, return the content. On `exit` or EOF, return a sentinel error. On malformed JSON, emit a `warning` and continue reading. `Emit`, `RequestApproval`, and `FinalText` stay unchanged.

### session/events.go

Add two new event types:

```go
type TurnDone struct {
    SessionID string `json:"sessionId"`
}

type Warning struct {
    Message string `json:"message"`
}
```

Update the `encode` switch to handle `TurnDone -> "turn_done"` and `Warning -> "warning"`.

### cmd/gohome/main.go

In the `if *prompt != ""` block:
- Detect `*prompt == "-"` and enforce `--verbose`.
- Pass `os.Stdin` to `headless.NewFrontend` when interactive.
- Replace the single `a.Run` call with a loop: call `AwaitUserInput`, append to history, call `a.Run`, emit `TurnDone`, repeat.
- On loop exit, emit `session_end` with appropriate reason.

### No changes to

- One-shot `--prompt "text"` path
- TUI path
- `Agent.Run`, `session.Load`, guard, tools
- `--resume` logic (already runs before the prompt check)

### Scope

~80-120 lines of new/modified code across 3 files.
