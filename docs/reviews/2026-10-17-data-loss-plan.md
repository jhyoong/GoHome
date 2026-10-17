# Plan: Data loss and broken sessions (D1-D9)

Source: section 3 of `2026-10-17-codebase-audit.md`. The code was checked on `922d8d4`, and all nine issues are still present.

All work goes in one PR. Each fix starts with a failing test that reproduces the audit evidence, then the fix.

## Decisions

1. **OpenAI stream with no `[DONE]`:** if a `finish_reason` was received, EOF counts as complete. Without a `finish_reason`, EOF is an error.
2. **Subagent sessions:** they are hidden from `--resume` and the `/resume` browser. When the user resumes a parent session, its subagent sessions are loaded too, so the full subagent session can still be viewed.
3. **One PR** covers all items.

## Session files (`internal/session`)

### 1. D1: Lines over 64 KiB
- In `load.go` and `list.go`, replace `bufio.Scanner` with `bufio.Reader.ReadBytes('\n')`, which has no line length limit.
- `List` logs unreadable files with `slog.Warn` instead of skipping them silently.
- Test: a session with a 70 KiB tool result loads and is listed.

### 2. D6: Replay groups tool results
- `Load` merges consecutive `tool_result` events into one `RoleTool` message, the same shape the live loop uses (`run.go:150-153`). This also fixes existing session files.
- `compact.go:87-89` steps back over every `RoleTool` message at the split point, not just one.
- Test: a resumed session with 2 parallel tool calls compacts without orphaned tool results.

### 3. D3 (load repair): Missing tool results
- After replay, every `tool_use` with no matching `tool_result` gets a synthetic `skipped` result. This repairs session files that are already broken. It goes in or next to `validateHistory`.
- Test: a file with a `tool_use` and no result loads with a valid tool_use/tool_result pairing.

### 4. D8: Write errors
- `Writer` keeps its first write, encode or sync error and returns it from a new `Err()` method.
- `runLoop` checks `Err()` after each run and shows a single "session not saved" error in the TUI.
- Test: a writer on a closed file reports an error.

### 5. D9: Emit after Close
- Add a `closed` flag guarded by a `sync.RWMutex`. `Emit` takes the read lock and drops (and logs) events after close. `Close` takes the write lock.
- Test: `Emit` after `Close` does not panic.

## Agent loop and streams (`internal/agent`, `internal/llm`)

### 6. D3 (live): Save skipped results
- In `run.go:104-112`, the steer-skip branch also writes `session.ToolResult` and emits `EventToolResult`. This saves the result and clears the "pending" tool entry in the TUI.
- Test: after a steer, the JSONL has one `tool_result` for each `tool_use`.

### 7. D2: Subagent ID collisions and resume
- Child IDs become `<parentID>-sub-N`. Parent IDs are already random (`session.NewID`), so file names are unique.
- `List` skips sessions with `Depth > 0`. `--resume` and the `/resume` browser then only offer top-level sessions.
- Add `session.ListChildren(home, cwd, parentID)`. It returns child listings with a matching `ParentID`, sorted by `StartedAt`.
- On `--resume` and `/resume`, load each child and add it as a session view, the same way live subagents appear. Link each child to the parent's `subagent` tool entries in order (subagents run one at a time). Children loaded this way are read-only.
- Old colliding `sub-N` files are left as they are.
- Tests: child IDs are unique across two agents. `List` excludes children. Resuming a parent loads its children and links them.

### 8. D5: Streams that end early
- Anthropic: EOF without `message_stop` emits `EventError("stream ended early")`.
- OpenAI: EOF without `[DONE]` counts as complete only if a `finish_reason` was received. Otherwise it is an error.
- `Turn`: if the channel closes without `EventTurnDone`, return an error instead of treating the turn as done (`turn.go`, "treat as end_turn").
- `max_tokens` and `length` stop reasons show a notice to the user.
- Tests: the truncated OpenAI tool call and the Anthropic stream with no `message_stop` from the audit both return errors.

### 9. D4: Invalid tool-call JSON
- `Turn` checks each tool call's input with `json.Valid`. If it is invalid, the input is stored as `{}` and the call is marked invalid. `Run` then returns an error result without running the tool: "arguments were truncated or invalid JSON, retry".
- Backstop: the Anthropic request builder (`anthropic/request.go:156-167`) replaces invalid `RawMessage` input with `{}`. This keeps already broken sessions usable.
- Tests: a truncated tool call does not run and does not break the next request. A history that already holds invalid input still encodes.

### 10. D7: Cancel during compaction
- `compact` applies the summary only if it received `EventTurnDone` and `ctx.Err() == nil`. Otherwise it returns an error and keeps the full history.
- Test: cancelling mid-stream leaves the history unchanged and writes no `Compaction` event.

## Docs
- Mark D1-D9 as fixed in the audit doc.
- Fix the stale comments in `session/load.go:17-24`, `session/list.go:23` and `agent/spawn.go:18-19`.

## Verification
- `go build`, `go vet ./gohome/...`, `go test ./gohome/...` and `golangci-lint run ./gohome/...` all pass.
- Manual test: resume a session that has a subagent and a long tool result, and confirm that the subagent view opens.
