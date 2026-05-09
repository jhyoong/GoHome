# Design: Tool Call Display, Stop, and Steering

Date: 2026-05-07

## Overview

Three features to improve the agent interaction experience:

1. **Tool call display** — show tool call inputs and outputs in the chat, collapsible on click
2. **Stop button** — cancel the running agent at any point
3. **Steering** — send messages to the agent while it is running, injected before its next LLM call

---

## Feature 1: Tool Call Display

### Goal

Tool call params and results are currently invisible on the frontend. They must appear inline in the chat for both loaded history and live runs.

### Frontend — history rendering

`ChatView.tsx` renders a `ToolCallBlock` component for each entry in `msg.tool_results` after the message's text content.

`ToolCallBlock` props: `toolName: string`, `params: string`, `result: string`, `approved: boolean`.

Behaviour:
- Collapsed by default — one line showing tool name and approved/denied status
- Click toggles expanded state (local `useState`)
- Expanded — JSON params in a `<pre>` block above, raw result in a `<pre>` block below

### Backend — real-time feedback

`loop.Run` receives a new callback: `onToolResult func(tool, params, result string, approved bool)`.

The loop calls `onToolResult` after each tool executes (approved or denied).

`runAgent` passes a callback that sends:

```json
{ "type": "tool_result", "tool": "...", "params": "...", "result": "...", "approved": true }
```

`app.tsx` handles `tool_result` events by appending a synthetic `ToolResult` entry to the messages list so results appear in real time.

---

## Feature 2: Stop Button

### Goal

Cancel the running agent mid-stream or mid-tool-execution via a Stop button in the UI.

### Backend

`wsConn` gains two fields protected by a `sync.Mutex`:
- `runCancel context.CancelFunc`
- `steerCh chan string`

When `runAgent` starts:
1. Create a child context with cancel derived from the WS context
2. Store the cancel func in `wsConn.runCancel`
3. Create a fresh `steerCh`
4. Clear both fields when the run exits

The dispatcher handles a new `"stop"` message type by calling the stored `runCancel`. Cancellation propagates through the LLM stream and any in-progress tool execution.

On run exit due to cancellation, the server sends:

```json
{ "type": "stopped" }
```

### Frontend

New types:
- `ClientMsg`: add `{ type: 'stop' }`
- `ServerMsg`: add `{ type: 'stopped' }`

UI changes:
- Stop button in the input bar, visible when `busy === true`
- Clicking sends `{ type: 'stop' }`
- On receiving `stopped`: set `busy = false`, clear `streamingContent`

---

## Feature 3: Steering

### Goal

Allow the user to send messages while the agent is running. The message is injected into the conversation history before the agent's next LLM call, redirecting it without stopping the run.

### Backend

`loop.Run` gains a `steerCh <-chan string` parameter.

At the top of each iteration, before the LLM call, the loop drains all pending messages from `steerCh`:
- Save each as a `user` message in the DB
- Append each to the in-memory history slice

The LLM then sees the steering message as a new user turn and adjusts its behaviour.

The dispatcher changes its handling of `"message"` while `busy == 1`:
- Old: return `{ type: "error", message: "busy" }`
- New: send content to `steerCh`

### Frontend

Input `disabled` condition changes from `busy || awaitingApproval !== null` to `awaitingApproval !== null`.

The input stays active whenever the agent is running. Messages sent while `busy === true` are added to the message list optimistically via the existing `handleSend` path. The backend routes them to `steerCh` instead of starting a new agent run.

---

## Data Flow Summary

```
User types while agent running
  → frontend sends { type: "message", ... }
  → dispatcher: busy? → send to steerCh
  → loop: top of next iteration → drain steerCh → append to history
  → LLM sees steering message on next call

User clicks Stop
  → frontend sends { type: "stop" }
  → dispatcher: calls runCancel()
  → context cancelled → LLM stream / tool exec aborts
  → server sends { type: "stopped" }
  → frontend: busy=false, streamingContent=""

Tool executes
  → loop calls onToolResult(tool, params, result, approved)
  → server sends { type: "tool_result", ... }
  → frontend appends to messages list
  → ToolCallBlock renders collapsed inline
```

---

## Files Changed

**Backend**
- `internal/agent/loop.go` — add `onToolResult` callback and `steerCh` parameter
- `internal/server/server.go` — per-run cancel/steer fields, stop handler, steer routing, tool_result event

**Frontend**
- `web/src/types.ts` — new ClientMsg and ServerMsg variants
- `web/src/app.tsx` — stop handler, tool_result handler, input always enabled
- `web/src/components/ChatView.tsx` — render ToolCallBlock per tool result, Stop button
- `web/src/components/ToolCallBlock.tsx` — new collapsible component
