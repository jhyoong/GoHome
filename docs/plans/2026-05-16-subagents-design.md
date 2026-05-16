# Subagents Design

## Overview

The main agent session can spawn subagent sessions to delegate tasks. Each subagent runs a full agent loop (LLM + tools) but cannot spawn further subagents. Subagent sessions are stored in the database linked to their parent, are hidden from the main session sidebar, and stream their activity into a collapsible block in the main chat.

## Database

Add `parent_session_id TEXT REFERENCES sessions(id) ON DELETE CASCADE` (nullable) to the `sessions` table.

- Sessions with `parent_session_id IS NULL` are main sessions.
- Sessions with a parent are subagent sessions.
- `store.ListSessions()` filters to `WHERE parent_session_id IS NULL`.
- New method: `store.CreateChildSession(ctx context.Context, parentSessionID string) (Session, error)`.

## Config

Add `subagent_system_prompt` field to `config.yaml`. If unset, the subagent runs with no system prompt.

```yaml
system_prompt: "You are a helpful assistant."
subagent_system_prompt: "You are a subagent. Complete the delegated task thoroughly and return your findings."
```

`server.Config` is refactored to hold `LLMClient`, `Registry`, `SystemPrompt`, and `SubagentSystemPrompt` directly instead of a pre-built `*agent.Loop`. The server builds loops per-run.

## SpawnSubagentTool

File: `internal/tools/spawn_subagent.go`

Implements the `Tool` interface. Parameters: `{"task": "string"}`.

Constructed per WebSocket connection with:
- `*llm.Client` — runs the subagent LLM loop
- `*tools.Registry` — global registry (no spawn tool), passed to the subagent to prevent recursion
- `*session.Store` — creates and persists the child session
- `*approval.Broker` — shared with the main agent; tool approvals go through the same user flow
- `send func(outMsg)` — streams events to the frontend
- `subagentSystemPrompt string` — from config
- `parentSessionID string` — set per-run

`Registry` gets a `CloneWith(tools ...Tool) *Registry` method returning a new registry with all existing tools plus the given ones.

In `server.go`, `runAgent` builds the `SpawnSubagentTool` fresh each run, clones the global registry with it, and creates a new `agent.Loop` for that run. The global registry is never mutated.

## Subagent Execution Flow

When `SpawnSubagentTool.Execute` is called:

1. Parse `task` from params.
2. Call `store.CreateChildSession(ctx, parentSessionID)` to get a child session.
3. Emit `subagent_start` to the frontend: `{type, session_id, parent_session_id}`.
4. Run `agent.NewLoop(llmClient, globalReg, store, subagentSystemPrompt).Run(...)` with callbacks:
   - `subagent_token` — streaming text tokens, tagged with `session_id`
   - `subagent_thinking_token` — thinking block tokens, tagged with `session_id`
   - `subagent_tool_result` — tool outcomes, tagged with `session_id`
   - Tool approvals go through the existing shared broker (`tool_approval`/`tool_response` WS messages, no changes needed).
5. On completion, emit `subagent_done`: `{type, session_id, message}` where `message` is the final text.
6. On error, emit `subagent_error`: `{type, session_id, message}`.
7. Return the subagent's final text as the tool result string to the main agent.

## Frontend Rendering

On `subagent_start`: open a collapsible block inside the current message stream, identified by `session_id`, with a running indicator labeled "Subagent".

`subagent_token`, `subagent_thinking_token`, and `subagent_tool_result` are routed into the block matching `session_id` and rendered the same as their main-session equivalents.

On `subagent_done`: replace the running indicator with a completed state. The block stays collapsible.

On `subagent_error`: render an error state inside the block.

The frontend tracks open subagent blocks in a map keyed by `session_id` to support multiple sequential subagents within one main session.
