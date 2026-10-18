# Follow-up Tasks

Open items found while fixing and testing the 2026-10-17 audit items (PRs #48 to #56). Each section names the fixes it came from. None of these are regressions.

## File permissions

15. **Existing `~/.gohome` folders created `0755` by older versions keep that mode.** Consider a one-time warning.

## Setup wizard

16. **No confirmation after the denylist step.** The wizard shows "step 9/10" and closes without saying that `denylist.json` was written.
17. **The wizard error line is not wrapped** to the terminal width.

## Logging

18. **`GOHOME_DEBUG=1` only logs "gohome started".** Guard decisions (whitelisted, denied, prompted) appear only in session files. Add debug log lines for guard decisions.

## Testing gaps

19. **The approval-prompt control-character warning was not tested manually.** The model removed raw ESC bytes from its tool calls. Covered by unit tests only.
20. **Bare `rm -rf /` forms were not run live.** The model refused them. They were tested as non-running forms (`false && ...`) and by calling the denylist check directly.

## Session files and streams (from the D1-D9 data loss fixes)

Minor items found while reviewing the D1-D9 fixes (plan: `docs/old/2026-10-17-data-loss-plan.md`). None of them lose data.

### Session loading
21. **Each line is copied twice.** `load.go` and `list.go` turn each line into a string and back into bytes. Pass the raw bytes to `json.Unmarshal` instead.
22. **`isBlank` reads the whole file.** It keeps reading after the first `user_message`. Stop at the first one.
23. **The compaction guard steps forward only once.** When the old messages would start with a `RoleTool` message, `compact.go` moves `prefixCount` by one. This is safe now that `Load` groups results, but a loop would be more robust.
24. **Repaired results come after the recorded ones.** The synthetic "skipped: no result recorded" results are added after the real results, so their order differs from the `tool_use` order. The API matches results by ID.
25. **Repair only checks the message right after each assistant turn.** No current code path writes anything else in between.

### Session writer
26. **`writer.Err()` is read before the write queue drains.** A write failure on the last turn is reported one run late, or never if the user quits. Consider checking `Close()`/`Err()` at shutdown.
27. **The newline is still written after a failed line write.** This can leave a broken line. `Load` skips lines that do not parse.
28. **Headless mode never checks `writer.Err()`.** Only the TUI `runLoop` shows "session not saved".
29. **Emit after Close logs at Debug level**, so it is not visible by default.

### Subagent sessions
30. **A child ID can be reused after a crash.** If gohome dies between the child's `session_start` and the parent's `subagent_spawn` marker, the next resume reuses `<parentID>-sub-N`. Fix by also taking the highest N from `ListChildren`.
31. **Fallback linking can still pick the wrong entry.** A resumed child whose task matches no entry is linked from newest to oldest. This can be wrong when unmatched children and unrelated unlinked entries both exist. Two subagents with the same task text can also swap.
32. **Task matching uses the sanitized entry text.** A task with invalid UTF-8 does not match and falls back to newest-to-oldest linking.

### Streams
33. **Tool entries stay "pending" after an early stream end.** Anthropic sends tool calls before `message_stop`, so the TUI shows them, but the turn is dropped. Mark pending entries as failed when `EventError` arrives. Cancelling mid-stream already did this before the fix.
34. **"stream ended early" is defined three times** (`anthropic/translate.go`, `openai/translate.go`, `agent/errors.go`). Use one sentinel in `common` so `errors.Is` works across packages.
35. **The `max_tokens`/`length` notice is not saved.** It is gone after a resume. `StopReason` is saved, so `historyToTimeline` could rebuild it.

### Testing gaps
37. **Weak assertions.** `stream_end_test.go` checks `err != nil` instead of the specific error. `countToolResultLines` matches substrings instead of decoding JSON. `TestWriterErrReportsWriteFailure` closes the file directly and ignores the double-close error.
38. **`TestPickResume_SkipsChildAndLoadsChildren` has no recorded failing run.** `TestListExcludesChildren` covers the same case and did fail first.

### Agent loop and TUI (found while fixing A1-A10)
40. **A failed run leaves the user message unanswered.** The message stays in history with no reply (A1 leftover). Decide whether to drop it or mark it.
41. **Sudo detection matches inside quoted strings.** `echo 'a; sudo b'` is treated as sudo. `VAR=/path/sudo cmd` (for example `SUDO=/usr/bin/sudo make install`) is also detected as sudo. The worst case is an extra password prompt. Fixing this needs a shell parser (`guard/sudo.go`).
42. **The sudo password dialog's expanded command (Tab) has no height cap or scroll.** The approval menu has both (A8).
44. **A resumed session's JSONL keeps the old model name.** The `SessionStart` record is not updated, although requests use the current model.

### LLM wire and config (found while fixing L1-L9)
45. **No prompt caching.** No `cache_control` is sent, and the Anthropic system prompt is a plain string.
46. **Mid-stream `overloaded_error` is not retried.** Only errors before the stream starts are retried.
47. **OpenAI reasoning models expect `max_completion_tokens`.** gohome always sends `max_tokens`. This is from training knowledge, not checked against live docs.
48. **A new tool-call id at an index already in use concatenates arguments.** Seen only from servers that do not follow the spec (`openai/translate.go`).
49. **SSE parsing does not handle bare-CR line endings or multi-line OpenAI `data:` payloads.** Both are rare.
50. **The idle-read watchdog can fire if the stream consumer stalls.** If the TUI or headless stdout is blocked for longer than the idle timeout, the stream is cut even though the server is still sending.

### Tools (found while fixing T1-T3)
51. **The shell `cwd` is resolved against the process folder, not the session folder.** This has no effect today: the session folder is always `os.Getwd()` and nothing calls `Chdir`. It matters only if sessions ever run in a different folder (audit T3).

### TUI (deferred from the R1-R23 fixes)
54. **Multi-line paste on Windows submits only the first line (audit R5).** Bubble Tea v1.3.10's Windows console input never sets `Paste`, so each pasted Enter submits. Fix with Bubble Tea v2 or a timing heuristic. Needs testing on Windows.
55. **The editor works per rune, not per grapheme (audit R9).** ZWJ emoji and combining marks are split by cursor movement and Backspace. Needs a rewrite of the editor's cursor model.
56. **Streaming re-parses the whole entry on every frame (audit R22).** The render cache is keyed on the full text, so a streaming assistant entry is parsed and wrapped again for each delta. Needs incremental markdown rendering.

### Dependencies (deferred from the outdated components fixes)
57. **Bubble Tea, Lip Gloss and Bubbles v2 are out at `charm.land/...`.** gohome is on Bubble Tea v1.3.10, Lip Gloss v1.1.0 and Bubbles v1.0.0. Moving to v2 is a migration with API changes. It may also fix item 54 (Windows paste) and allow Shift+Enter.

### CI and release (found while fixing the docs and CI items)
63. **The rewritten `release.yml` has not run.** Native Windows build, per-target size guard and CHANGELOG-section release notes are only YAML-checked. Push a test tag (for example `v0.4.4-dev2`, with a matching CHANGELOG section) and check the draft release and its four assets.

### Compaction (found while fixing items 58-62)
64. **A summary cut off by the output limit is still applied.** `compact()` ignores the stop reason. With `max_tokens` or `length`, the summary may be cut off, or empty when a reasoning model spends the whole limit on thinking (then compaction is skipped with "empty summary"). Decide whether to reject a cut-off summary, retry with a larger limit, or keep it. Found while checking item 59: with `MaxTokens` 256, 2 of 3 summaries stopped with `length` and no text.

### Terminal output (found while fixing items 11-13)
65. **The editor and live user entries show raw control characters.** Picking a file whose name holds an escape sequence from the file search list inserts the real path into the editor, and the editor prints it raw (an SGR color applied in the live test). Text typed or pasted into the editor, and the user timeline entry made from it, are also not sanitized, so a pasted U+202E or ESC reaches the terminal. Resumed sessions are not affected (`historyToTimeline` sanitizes). This code is unchanged from `main` (not re-run there).

### Compaction (found in the fix/approval-whitelist live test)
66. **Compaction can run with little or no gain.** On `local-direct-anthropic`, one compaction went from 114 to 125 tokens, and others saved only a few tokens (94 to 88, 146 to 136 on `main`). One run compacted 9 times in 5 turns. Consider skipping compaction when the history is already small, or rejecting a summary that is not shorter than what it replaces. Found in `docs/reviews/2026-10-18-fix-approval-whitelist-live-test.md`; also on `main`.
