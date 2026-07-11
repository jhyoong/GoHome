# Denial Return-to-User Design

## Problem

When a user denies a tool call, the denial result is sent directly back to the LLM
and the agent loop continues to the next turn. The user has no opportunity to provide
corrective input. This leads to two failure modes:

1. The LLM retries similar commands despite the denial.
2. When the user provides a steer message during the approval prompt, the LLM sometimes
   ignores it.

The root cause is that a plain denial returns an error tool result ("Tool call denied by
user.") without any user-authored context, and the LLM immediately takes another turn.

## Design

### Approach: sentinel return from Run()

When any tool call in a batch is plain-denied (no steer message), `Run()` breaks out of
the agent loop and returns a sentinel error `ErrToolDenied`. The caller (`runLoop` in
main.go) already loops back to `AwaitUserInput()` after `Run()` returns, so the user
naturally gets control of the editor. Their next message is appended as `RoleUser` after
the denial tool results, giving the LLM both the denial and the user's corrective input.

### Deny+steer is unchanged

When the user provides a steer message (option 4 in the approval prompt), the steer
message is included in the tool result and the agent loop continues to the next LLM turn.
The user already provided guidance, so there is no need to return control.

### Session history after denial

After a plain denial, the session history looks like:

```
[RoleAssistant: tool_use "rm -rf /"]
[RoleTool: "Tool call denied by user." isError=true]
[RoleUser: "Don't delete files. List the directory instead."]
```

The LLM sees the denial, the error flag, and the user's corrective instruction in
sequence.

### Multi-tool batch behavior

When the LLM requests multiple tool calls in one turn:

1. All tool calls go through the approval queue (FIFO, as today).
2. Approved tools execute normally, denied tools get denial results.
3. All results (both successful and denied) are appended to history as RoleTool.
4. After the batch, if any tool was plain-denied, Run() returns ErrToolDenied.
5. If all denials had steer messages, the loop continues to the LLM.
6. If one tool is plain-denied and another is deny+steer, the plain deny takes
   precedence and Run() returns ErrToolDenied. The steer message is preserved in
   the tool result for that call.

### TUI notification

Before Run() returns ErrToolDenied, it emits an EventToolDenied frontend event. The TUI
renders this as a notice timeline entry: "Tool call denied. Waiting for your input."

## Changes

### Modified files

| File | Change |
|------|--------|
| `gohome/internal/agent/run.go` | `dispatchTool` returns new `denied bool`. `Run()` tracks denials across the batch. Returns `ErrToolDenied` if any plain denial. Emits `EventToolDenied` before returning. |
| `gohome/internal/agent/event.go` | Add `EventToolDenied` event kind. |
| `gohome/internal/agent/errors.go` | New file. Define `var ErrToolDenied`. |
| `gohome/cmd/gohome/main.go` | `runLoop` handles `ErrToolDenied` with `continue` (skip error logging). |
| `gohome/internal/tui/model.go` | Handle `EventToolDenied` in event handler, append notice timeline entry. |
| `gohome/internal/agent/run_test.go` | Update `TestRun_DeniedTool` to expect `ErrToolDenied`. Add test for mixed batch. Add test confirming deny+steer does NOT return `ErrToolDenied`. |

### Unchanged

- Guard, approval UI, approval prompt options
- Deny+steer flow (steer message goes to LLM, loop continues)
- Session persistence (approval events still written)
- Tool result format ("Tool call denied by user." with isError: true)
