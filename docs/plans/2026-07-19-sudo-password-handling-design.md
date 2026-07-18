# Sudo Password Handling

## Problem

When the LLM emits a shell command containing `sudo`, the command fails because the shell tool provides no stdin (reads from `/dev/null`) and allocates no TTY. `sudo` either immediately exits with "a terminal is required to read the password" or hangs until the shell timeout fires. The LLM sees a vague failure, retries with variations, and the session spirals into a loop of failed attempts.

## Design

### Detection

The guard layer detects when a shell command contains `sudo` at a command boundary. A regex like `(^|[;&|]\s*)sudo\s` matches realistic cases:

- `sudo apt install ...`
- `echo foo | sudo tee ...`
- `cd /tmp && sudo rm ...`

Without over-matching embedded occurrences like `sudoers` in a grep pattern.

When detected, `ApprovalRequest.NeedsSudoPassword` is set to `true` so the TUI knows to show a password field.

### Collection

The approval prompt gains a masked password input field (dots) that appears only for sudo commands. The field is rendered below the command summary. The user enters the password as part of the approve action. The password is included in the `ApprovalDecision` returned to the guard/agent layer.

### Delivery

The shell tool receives the password via the execution path. It rewrites `sudo` to `sudo -S` in the command string and pipes `password\n` into `cmd.Stdin`. The `-S` flag tells sudo to read the password from stdin rather than requiring a TTY.

### Caching

After a successful sudo execution (exit code 0), the password is cached in memory on the TUI `Model` as `sudoPasswordCache`. Subsequent sudo approval prompts auto-fill the password from cache. The user can still override it by typing a different password.

The cache is cleared when the session ends. The password never touches disk.

## Component Changes

1. **`guard.ApprovalRequest`** -- Add `NeedsSudoPassword bool` field.
2. **`guard.ApprovalDecision`** -- Add `SudoPassword string` field.
3. **`guard/check.go`** -- Detect sudo in the command and set `NeedsSudoPassword` on the request.
4. **`tui/approval.go`** -- `approvalPrompt` gains a `passwordInput textinput.Model` with `EchoMode = EchoPassword` (masked dots). Rendered below the command summary when `NeedsSudoPassword` is true.
5. **`tui/model.go`** -- Add `sudoPasswordCache string` field. Populated on first successful sudo, auto-filled on subsequent prompts.
6. **`tui/model_approval.go`** -- When resolving an approval for a sudo command, include the password (from input or cache) in the decision.
7. **`agent/run.go`** -- `dispatchTool` passes the sudo password through to the shell tool execution. This requires threading the password from the guard decision into the tool execution path, either via context value or a wrapper on the tool input.
8. **`tools/shell.go`** -- When a password is provided, inject `-S` after `sudo` in the command string and write `password\n` to `cmd.Stdin` before the process starts.
9. **`tui/model.go`** -- On receiving a successful tool result for a sudo command, update `sudoPasswordCache`.

## Security Constraints

- Password lives only in TUI memory (`sudoPasswordCache`), never serialized.
- Password is not sent to the LLM in any tool result or history message.
- Password is not written to JSONL session files (the `Approval` event struct does not include it).
- Password is not included in debug logs.
- Cache is cleared on session end.

## Sudo Detection Regex

```
(^|[;&|]\s*)sudo\s
```

This matches `sudo` preceded by start-of-string or a shell separator (`; & |`), followed by whitespace. It avoids matching `sudo` embedded in other words.
