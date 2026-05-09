# Context Window Tracking — Design

## Overview

Track token usage per conversation turn and display it in the frontend as `50k / 131k (38%)` below the message input. Usage is read from the LLM API's streaming response; the max context window is configured per endpoint.

## Approach

Use `stream_options: {include_usage: true}` in streaming requests. OMLX sends a final SSE chunk with a `usage` field (prompt_tokens, completion_tokens, total_tokens) before `[DONE]`. An `onUsage` callback in `Stream()` fires when this chunk is parsed, flowing the data up through the agent loop and server to the frontend via a new WebSocket message type.

## Components

### Config (`internal/config/config.go`)

Add `context_window int` to `EndpointConfig`:

```go
ContextWindow int `yaml:"context_window"`
```

In `Load()`, default to `131072` if the field is 0 (not set by the user).

### LLM Client (`internal/llm/client.go`)

- Add `StreamOptions *streamOptions` to `reqBody`. Set it to `{include_usage: true}` when streaming.
- Add `onUsage func(promptTokens, completionTokens, totalTokens int)` as a new parameter to `Stream()`.
- In the SSE scanner loop, detect the usage chunk (empty `choices`, non-nil `usage`) and call `onUsage`.

### Agent Loop (`internal/agent/loop.go`)

- Add `onUsage func(prompt, completion, total int)` to `Loop.Run()`.
- Pass a forwarding closure as `onUsage` to each `llm.Stream()` call inside the loop.
- Pass `nil` for `onUsage` on the `GenerateTitle` call (not needed there).

### Server (`internal/server/server.go`)

- Add `ContextWindow int` to `server.Config`, populated from `cfg.Endpoint.ContextWindow`.
- Add `PromptTokens`, `CompletionTokens`, `ContextWindow` fields to `outMsg`.
- In `runAgent`, pass an `onUsage` callback that sends:
  ```json
  { "type": "usage", "prompt_tokens": N, "completion_tokens": N, "context_window": N }
  ```

### Frontend

**`web/static/index.html`**: Add below `<form id="input-form">`:
```html
<div id="context-usage" class="context-usage" hidden>
  <span id="context-usage-text"></span>
</div>
```

**`web/static/app.js`**:
- Add `contextUsage` and `contextUsageText` to `dom` refs.
- Handle `case 'usage'` in `ws.onmessage` — call `updateContextUsage(msg)`.
- `updateContextUsage` formats values as `Xk / Yk (Z%)` and unhides the element.
- Hide and reset the counter on new chat or session load.

**`web/static/app.css`**: Small, muted, right-aligned text below the input bar.

## Data Flow

```
LLM API (stream_options) → Stream() onUsage callback
  → Loop.Run() onUsage callback
    → Server runAgent onUsage callback
      → WebSocket "usage" message
        → Frontend updateContextUsage()
          → DOM update below input bar
```

## Non-Goals

- Persistent token history across sessions (display only, not stored)
- Per-turn breakdown (total context used is sufficient)
- Token estimation fallback if the API omits usage
