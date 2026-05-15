# Thinking Blocks Rendering — Correct Sequence

## Problem

When an agent makes multiple LLM calls (because it uses tools), the thinking blocks from each iteration are collapsed into a single block rather than shown in the correct sequence. The desired order is:

```
Thinking (iteration 1) → Tool call → Thinking (iteration 2) → Final text
```

There are two separate failure points:

1. **Backend**: `thinkingText` is only saved on the final assistant message. For tool-call iterations, the thinking is streamed to the client but discarded from the DB. On session reload, all per-iteration thinking is lost.

2. **Frontend streaming**: `addToolResult()` flushes text content when a tool result arrives but leaves `streamingThinkingEl` alive. Thinking tokens for the next iteration append to the same div. Also, `handleThinkingToken` searches the entire DOM for `.message-thinking` instead of using the module-level `streamingThinkingEl`, causing it to reuse already-finalized thinking elements.

## Approach

Approach B: save thinking per loop iteration, render inline. No DB schema change required — the `thinking` column already exists on every `messages` row.

## Design

### Backend (`internal/agent/loop.go`)

When tool calls are found and the tool-call assistant message is saved, include `thinkingText` in that `AddMessage` call:

```go
assistantMsg, err := l.store.AddMessage(ctx, session.Message{
    SessionID: sessionID,
    Role:      "assistant",
    ToolCalls: string(tcJSON),
    Thinking:  thinkingText,   // <-- add this
})
```

The final text message already saves its thinking correctly. No other backend changes needed.

### Frontend — streaming (`web/static/app.js`)

**`addToolResult()`**: After flushing the text preamble, also finalize the current thinking block. If `streamingThinkingEl` has content, convert it to a standalone rendered thinking block appended to the DOM, then set `streamingThinkingEl = null`. This ensures the next iteration's thinking tokens create a new block.

**`handleThinkingToken()`**: Replace the `dom.messages.querySelector('.message-thinking')` DOM search with a check against `streamingThinkingEl` (the module-level variable). The DOM search incorrectly picks up already-finalized thinking divs from previous iterations.

### Frontend — history rendering (`web/static/app.js`)

`msgHtml()` already renders `${thinkingBlocks}${content}${toolBlocks}` in order. Since thinking is now saved on each message (including tool-call messages), reload order will naturally be: thinking → tool calls, or thinking → text. No change to `msgHtml` needed.

## Sequence After Fix

**Live streaming:**
1. `thinking_token` events → new thinking block created and streamed
2. `tool_result` event → thinking block finalized, `streamingThinkingEl` reset to null; tool block appended
3. `thinking_token` events (next iteration) → fresh thinking block created
4. `token` events → text content streamed
5. `done` event → final message finalized

**Session reload:**
- Tool-call message: renders thinking block then tool blocks (correct order)
- Final message: renders thinking block then text (correct order)
