# What to focus on

## Next PR: streams, LLM wire, prompt caching and wizard wrapping

Branch: `fix/llm-wire-streams-caching`. FOLLOWUP_TASKS items 33, 34, 35, 37 (stream test part), 45, 46, 47, 48, 49, 50 and 67.

Work in this order. Each step is one commit.

### 1. Stream end (33, 34, 35, 37)

1. **34 One sentinel for "stream ended early".** Add `ErrStreamEndedEarly` to `llm/common` (next to `ErrStreamIdle` in `common/stream.go`). Use it in `anthropic/translate.go:112`, `openai/translate.go:155` and `agent/turn.go`, `agent/compact.go`. Remove `agent.ErrStreamEndedEarly` (no alias).
   - Test: an early end from each adapter, and from `Turn`, matches `errors.Is(err, common.ErrStreamEndedEarly)`.
2. **37 Stronger stream-end assertions.** `stream_end_test.go` checks `errors.Is` with the sentinel instead of `err != nil`.
3. **33 Pending tool entries after an early end.** On `agent.EventError` (`tui/model_agent.go:244`), mark the session's non-shadow tool entries with `Status == "pending"` as `"error"`.
   - Test: a tool entry shown from a streamed tool call, then `EventError`, renders as failed, not pending.
4. **35 Rebuild the length notice on resume.** In `historyToTimeline` (`tui/history_convert.go`), an assistant message with `StopReason` `max_tokens` or `length` adds the same notice `Turn` emits ("Response cut off: ..."). Share the text through one function so live and resumed notices match.
   - Test: history with such a message gives the notice entry after it.

### 2. Retry and watchdog (46, 50)

5. **46 Retry a mid-stream `overloaded_error` before any content.** Anthropic sends `event: error` with `{"type":"error","error":{"type":"overloaded_error",...}}`.
   - In `anthropic/translate.go`, decode the error frame. For `overloaded_error`, wrap a new `common.ErrOverloaded`.
   - In `Turn`, if the error wraps `ErrOverloaded` and no text, thinking or tool block has arrived yet, retry the request using the client's existing backoff (`common/retry.go`). Once content has arrived, fail as today (no partial-turn replay).
   - Test: overloaded error as the first frame retries and succeeds; overloaded error after a text delta fails with the error.
6. **50 Idle watchdog and a slow consumer.** In `StreamRequest` (`common/stream.go`), stop the idle timer before the blocking `out <- e` send and reset it after the send. A blocked TUI or stdout no longer counts as server silence.
   - Test: with a short idle timeout, a consumer that waits longer than the timeout before reading still gets the full stream; a server that stops sending still trips the timeout.

### 3. SSE parsing and OpenAI wire (49, 48, 47)

7. **49 One SSE parser.** Move to a shared `common/sse.go` parser used by both adapters.
   - Accept LF, CRLF and bare CR line endings.
   - Join multiple `data:` lines in one event with `\n` (SSE spec). OpenAI keeps working because each OpenAI event has one `data:` line.
   - Keep the `event:` field for Anthropic.
   - Tests: move the existing `sse_test.go` cases to common; add bare CR, CRLF and a two-line `data:` event.
8. **48 New tool-call id at a used index.** In `openai/translate.go`, when a delta at an index already in use carries a different non-empty `id`, finish the current call and start a new one instead of joining the arguments.
   - Test: two calls with the same index and different ids give two tool calls with their own arguments.
9. **47 `max_completion_tokens` per model.** New optional model setting `maxTokensField` (`ModelConfig`, `config/config.go`). Allowed values: `max_tokens` (default) and `max_completion_tokens`. The OpenAI adapter sends the limit under that name. Ignored for the Anthropic wire. Invalid values are a config error.
   - Docs: README model config table and example.
   - Tests: request body for each value; config validation rejects other values.

### 4. Prompt caching (45)

10. **45 Opt-in Anthropic prompt caching.** New optional model setting `promptCache` (bool, default false). When true on the Anthropic wire:
    - Send `system` as one text block with `cache_control: {"type": "ephemeral"}` (tools render before system, so this caches tools and system together).
    - Add `cache_control: {"type": "ephemeral"}` to the last content block of the last message, so each turn reads the previous turn's prefix.
    - Two breakpoints only (the API allows four). No top-level automatic `cache_control`, since some proxies reject it. No 1-hour TTL.
    - When false, the request body is unchanged (string `system`, no `cache_control`).
    - Usage already parses `cache_read_input_tokens` and `cache_creation_input_tokens`; check that the TUI stats show them.
    - Docs: README model config table and a short note that prompts under the model's minimum length (512-4096 tokens) are not cached.
    - Tests: request body with and without `promptCache`; marker lands on the last block when the last message ends with tool results.

### 5. Wizard wrapping (67)

11. **67 Wrap wizard option descriptions and notices.** At 50 columns, option descriptions in the select list (`tui/selectlist.go`) and the notice line behind the wizard are cut. Wrap them to the width instead.
    - Test: snapshot or render test at 50 columns shows the full description over two lines.

### Then

- Remove items 33, 34, 35, 37 (stream part), 45, 46, 47, 48, 49, 50 and 67 from `docs/FOLLOWUP_TASKS.md`. Add CHANGELOG Unreleased entries (Added: `maxTokensField`, `promptCache`).
- Each fix gets a failing test first. Run `go vet`, `go test`, `golangci-lint`. Update snapshots only if output changes on purpose.
- Live test with the gohome-full-test skill on both local models:
  - Normal turns, tools and compaction still work.
  - With `promptCache: true` on `local-direct-anthropic`, check whether the endpoint accepts the markers and reports cache tokens. Record the result either way.
  - With `maxTokensField: "max_completion_tokens"` on `local-openai`, a turn still respects the limit.

Out of scope: 19, 20, 38, 41, 51, 54-57, 63.
