# Thinking Block Consistency

Unify how thinking/reasoning blocks behave across Anthropic and OpenAI wire protocols for session persistence, TUI display, and resume.

## Problem

Three inconsistencies exist in how thinking blocks are handled:

1. Resumed thinking blocks default to expanded (`Expanded: true` in `history_convert.go`), creating visual noise when loading old sessions.
2. Live thinking blocks stay expanded after `EventThinkingDone` fires -- they are never collapsed.
3. No validation runs on loaded session data, so malformed thinking blocks (empty text, corrupt signatures) pass through silently.

The underlying architecture is already consistent: both adapters produce the same `BlockThinking` / `EventThinkingDelta` / `EventThinkingDone` event types, and both persist `BlockThinking` blocks to JSONL. The issues are in default state and missing validation.

## Changes

### 1. Thinking block expansion state

- `history_convert.go`: set `Expanded: false` on `KindThinking` entries created from resumed sessions.
- `model.go` `handleAgentEvent`: on `EventThinkingDone`, set `Expanded = false` on the most recent `KindThinking` timeline entry. This collapses the live thinking block after reasoning finishes.

Both live and resumed thinking blocks end up collapsed. The user can expand with Enter.

### 2. Session load validation

- Add a validation function that runs after JSONL is parsed into `[]common.Message` and before messages are handed to the TUI or agent.
- For each `BlockThinking`:
  - Check `Text` is non-empty. Log `slog.Warn` if empty.
  - Do not reject blocks with empty `Signature` -- OpenAI-wire sessions legitimately produce them.
- Malformed entries are logged but not removed. The session still loads.

### 3. Cross-protocol resume tests

- `agent/turn_test.go`: verify thinking blocks are persisted correctly with and without signatures (Anthropic vs OpenAI cases).
- `tui/history_convert_test.go`: verify `historyToTimeline` produces `KindThinking` entries with `Expanded: false` for both signature and no-signature blocks, and for empty-text blocks.
- Validation function tests: valid blocks produce no warnings, empty-text blocks produce warnings, all blocks are preserved regardless.

## Out of scope

- Sending thinking blocks back to OpenAI-wire providers on resume (silently skipped, per current behavior).
- Supporting additional reasoning field names beyond `thinking` and `reasoning_content`.
- Visual transition/animation on thinking block collapse.
