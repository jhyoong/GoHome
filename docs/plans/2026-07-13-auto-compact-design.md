# Auto-Compact Design

## Overview

Automatic context compaction for gohome sessions. When token usage approaches the context window limit, the agent summarizes the conversation history via an LLM call and replaces it with the summary, freeing context space for continued work.

## Requirements

- LLM-based summarization of conversation history
- Automatic trigger (no user confirmation), with a config toggle to enable/disable
- Two trigger modes (user's choice):
  - **Percentage mode**: triggers when usage exceeds a configured percentage of the context window
  - **Leftover mode**: triggers when free tokens drop below a configured threshold
- Show a TUI notice when compaction occurs
- Configurable summarization prompt
- Original history preserved in JSONL (append-only, no data loss)
- Session resume must correctly handle compaction events

## Configuration

New fields in `Settings` (settings.json):

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `autoCompact` | bool | `false` | Enable/disable auto-compact |
| `autoCompactMode` | string | `"percentage"` | `"percentage"` or `"leftover"` |
| `autoCompactPct` | float64 | `0.80` | Trigger when usage ratio >= this (percentage mode) |
| `autoCompactTargetPct` | float64 | `0.50` | Target usage ratio after compaction (percentage mode) |
| `autoCompactLeftover` | int | `32000` | Trigger when free tokens < this (leftover mode) |
| `autoCompactPrompt` | string | (built-in) | Custom summarization prompt |

Merge behavior: project settings override global, same as all existing fields.

### Example: percentage mode

```json
{
  "autoCompact": true,
  "autoCompactMode": "percentage",
  "autoCompactPct": 0.80,
  "autoCompactTargetPct": 0.50
}
```

### Example: leftover mode

```json
{
  "autoCompact": true,
  "autoCompactMode": "leftover",
  "autoCompactLeftover": 32000
}
```

## Architecture

### Approach: agent-layer compaction

Compaction logic lives in the `agent` package. A check runs in `Agent.Run()` after each `Turn()` completes and usage is known. If the threshold is exceeded, a summarization LLM call is made and `sess.History` is replaced.

### New types

```go
// agent/compact.go

type CompactConfig struct {
    Enabled       bool
    Mode          string  // "percentage" or "leftover"
    TriggerPct    float64
    TargetPct     float64
    Leftover      int
    ContextWindow int
}
```

`CompactConfig` is a new field on `Agent`, populated from `Settings` in `main.go`.

### Trigger logic

```
shouldCompact(usage):
  if not enabled: return false
  used = usage.InputTokens + usage.OutputTokens
  if mode == "percentage":
    return used / contextWindow >= triggerPct
  if mode == "leftover":
    return (contextWindow - used) < leftover
```

### Compaction procedure

1. Build a summarization request with the configured (or default) summarization prompt as the system message and `sess.History` as the messages.
2. Stream the LLM response and collect the summary text.
3. Replace `sess.History` with a single user message containing `[Auto-compact summary]\n\n<summary text>`.
4. Emit `EventCompacted` to the frontend with before/after token counts.
5. Persist a `compaction` event to the JSONL writer.
6. Reset context warning flags so they can fire again.

Compaction is non-fatal: if the summarization call fails, the agent logs a warning and continues with uncompacted history.

### Integration point

In `Agent.Run()`, after `Turn()` returns and before tool dispatch:

```
for {
    Turn(ctx, sess)
    
    // --- compact check here ---
    if shouldCompact(latestUsage) {
        compact(ctx, sess)  // non-fatal on error
    }
    
    // existing tool dispatch logic
    ...
}
```

The usage for the compact check comes from the `Turn()` return -- specifically the `Usage` field from the `EventTurnDone` stream event.

## Persistence

### JSONL format

A `compaction` event is appended to the session JSONL file:

```json
{
  "type": "compaction",
  "ts": "2026-07-13T10:30:00Z",
  "beforeTokens": 95000,
  "afterTokens": 40000,
  "summary": "The full summary text..."
}
```

The original messages above this event remain in the file. Nothing is deleted.

### Session resume

`session.Load` is updated to recognize `compaction` events during JSONL replay:

- When a `compaction` event is encountered, discard accumulated history and replace with the summary message.
- Continue replaying any messages that follow the compaction event.
- If multiple compactions occurred, each resets -- only the last compaction's summary plus subsequent messages form the final history.

This means:
- Full original conversation is always preserved in the JSONL file
- Resumed sessions get the compacted view
- JSONL is append-only with no data loss

## TUI Integration

### New agent event

```go
EventCompacted EventKind = "compacted"
```

The `Event` struct gains `CompactBefore` and `CompactAfter` int fields.

### TUI handling

In `model_agent.go`, when `EventCompacted` is received:
- Append a `KindNotice` timeline entry: `"Context compacted: 95k -> 40k tokens"`
- Reset `sv.warned80` and `sv.warned95` flags
- Usage is updated on the next `EventUsageUpdated` after the post-compaction turn

### Status bar

No changes. The token counter naturally reflects updated usage after the next turn.

## Default summarization prompt

```
You are summarizing a coding assistant conversation for context compaction.
Produce a concise summary that preserves:
- The user's current goal and any sub-tasks
- Key decisions made and their reasoning
- File paths and code changes discussed or made
- Any pending work or unresolved issues
- Tool results that are still relevant

Be factual and specific. Do not add commentary or analysis.
Write the summary as a narrative, not a bulleted list.
```

Configurable via `autoCompactPrompt` in settings.json.

## Testing

### Unit tests

- `agent/compact_test.go`:
  - `TestShouldCompact_Percentage` -- trigger logic at various usage levels
  - `TestShouldCompact_Leftover` -- trigger logic for leftover mode
  - `TestShouldCompact_Disabled` -- no trigger when disabled
  - `TestCompact_ReplacesHistory` -- history replaced with summary (fake client)
  - `TestCompact_EmitsEvent` -- EventCompacted emitted with correct counts
  - `TestCompact_NonFatalOnError` -- agent continues on summarization failure

- `session/load_test.go`:
  - `TestLoad_WithCompaction` -- JSONL with compaction event loads correctly
  - `TestLoad_MultipleCompactions` -- only last compaction used

- `config/config_test.go`:
  - Merge behavior for new auto-compact fields

- TUI snapshot tests updated if notice rendering changes

### No E2E tests

Auto-compact requires LLM calls; E2E tests require a live endpoint and are not run in CI.

## Files changed

| File | Change |
|------|--------|
| `internal/config/config.go` | Add auto-compact fields to `Settings`, merge logic, annotated sources |
| `internal/config/defaults.go` | Add default constants |
| `internal/agent/agent.go` | Add `CompactConfig` field, `CompactPrompt` field |
| `internal/agent/compact.go` | New file: `CompactConfig`, `shouldCompact()`, `compact()` |
| `internal/agent/run.go` | Insert compaction check after `Turn()` |
| `internal/agent/events.go` | Add `EventCompacted`, compact fields on `Event` |
| `internal/session/events.go` | Add `Compaction` event type |
| `internal/session/load.go` | Handle compaction events during replay |
| `internal/tui/model_agent.go` | Handle `EventCompacted`, render notice, reset warnings |
| `cmd/gohome/main.go` | Wire `CompactConfig` from settings into agent |
| Tests | New and updated test files as described above |
