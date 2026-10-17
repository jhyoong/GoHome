# LLM Wire and Config Fix Plan - 2026-10-17

Plan for section 5 (L1-L9) of `2026-10-17-codebase-audit.md`. Branch: `fix/llm-wire-configs`.

## Status at planning time

All of L1-L9 are open. Earlier PRs touched only nearby code: D5 added early-stream-end errors, and D4 made invalid tool JSON safe. FOLLOWUP #43 ("a config cannot turn thinking off") is the same issue as L1.

| ID | What is still wrong |
|---|---|
| L1 | `main.go` `applyModelConfig` turns a budget of 0 or less into 10240. Every Anthropic request sends `{type:"enabled", budget_tokens}`, which current Claude models reject with a 400 (Opus 5.5, Sonnet 5.5, Fable 5/5.1, Opus 4.7/4.8). There is no adaptive mode and no check that the budget is below `max_tokens`. |
| L2 | `agent/turn.go` merges all thinking into one block with the last signature. It drops thinking blocks with empty text, which current models send by default (`display: "omitted"`). |
| L3 | `redacted_thinking` blocks are not handled. |
| L4 | 429, 408 and 409 are not retried. `Retry-After` is ignored and there is no jitter. Error bodies are read with no size limit. Both clients use a plain `&http.Client{}`. |
| L5 | OpenAI `{"error":...}` chunks are ignored. |
| L6 | The skeleton writes `"defaultModel": "string"` and an `example` model. |
| L7 | Invalid JSON is treated as an empty file. A project file cannot set booleans to false. `retryBackoffMs` and `autoCompactTargetPct` do nothing. |
| L8, L9 | Small items. |

## Decisions

- When a model config does not set `thinking`, the Anthropic wire sends adaptive thinking. Old configs with `thinkingBudget > 0` keep budget mode.
- Prompt caching (`cache_control`) is not part of this PR. It goes to FOLLOWUP_TASKS.
- Mid-stream retries and the bare-CR SSE parsing are not part of this PR.
- No live E2E tests against public APIs. A live check uses the local endpoint only.

## Tasks

Each task follows the same steps: write a failing test, fix the code, then run `go test`, `go vet` and `golangci-lint`.

### Task 1: Thinking modes (L1, FOLLOWUP #43)
- Add `thinking` to `ModelConfig`, with these values:
  - `off`: send no thinking parameter.
  - `adaptive`: send `{type:"adaptive", display:"summarized"}`.
  - `budget`: send `{type:"enabled", budget_tokens}`. The budget defaults to 10240.
  - unset: use `budget` if `thinkingBudget > 0`, otherwise `adaptive`.
- On the Anthropic wire, send `reasoningEffort` as `output_config.effort`.
- In `budget` mode, check at startup and on `/model` that the budget is at least 1024 and below `maxTokens`. If not, show a clear error.
- Replace `ThinkingBudget` in `common.Request` and `agent.ModelSettings` with a mode and a budget.

### Task 2: Keep thinking blocks correct (L2, L3)
- `Turn` builds content blocks in the order they arrive. Each thinking block keeps its own signature.
- A thinking block is saved when it has text or a signature. Empty blocks with no signature (OpenAI) are skipped.
- Add `BlockRedactedThinking` with a `Data` field. Read it from `content_block_start`, save it to the session, and send it back unchanged.
- The TUI history conversion, `session/validate.go` and the OpenAI request builder skip redacted blocks.

### Task 3: Retry and timeouts (L4)
- Retry 408, 409, 429 and 5xx (including 529), as well as connection errors.
- Use `Retry-After` (seconds or an HTTP date) when present, capped at 60 s. Add jitter to the backoff.
- Cap error-body reads at 64 KiB in `retry.go` and `stream.go`.
- Use a transport with a response-header timeout, plus an idle-read watchdog that cancels a stream when no bytes arrive for a set time. Both default to about 5 minutes. Do not set a total `Client.Timeout`.

### Task 4: OpenAI wire (L5, part of L8)
- Treat `{"error":...}` chunks as `EventError`.
- Also read `delta.reasoning`.
- Send `content: ""` instead of `null` for an assistant turn that has only thinking.
- Join multiple user text blocks with `\n`.

### Task 5: Config (L6, L7)
- The skeleton keeps all top-level keys with empty or zero values. Remove the `example` model and the `"string"` values.
- Invalid JSON returns an error from `Load`. `main` exits with the file path and the parse error, so the wizard never overwrites `settings.json`.
- Decode `autoCompact` and `cacheSudoPassword` as `*bool` while loading, so a project file can set them to `false`. `Settings` keeps plain `bool`.
- Pass `retryBackoffMs` to both clients through `llm.New`. Fall back to `config.DefaultRetryBackoff` and remove `common.DefaultBackoff`.
- Remove `autoCompactTargetPct` from settings, the skeleton, the config overlay, `CompactConfig` and the README.

### Task 6: Small items (L9)
- Compaction counts cache-read and cache-write tokens.

### Task 7: Docs
- README config table: `thinking`, `reasoningEffort` on the Anthropic wire, `retryBackoffMs` now works, `autoCompactTargetPct` removed.
- CHANGELOG "Unreleased" entry.
- Mark L1-L9 in the audit with their status.
- Add to FOLLOWUP_TASKS and remove FOLLOWUP #43:
  - prompt caching (`cache_control`)
  - mid-stream `overloaded_error` retry
  - `max_completion_tokens` for OpenAI reasoning models
  - tool-call id reuse at the same index
  - bare-CR and multi-line OpenAI SSE parsing
