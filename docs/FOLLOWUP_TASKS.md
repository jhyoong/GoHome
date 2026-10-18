# Follow-up Tasks

Open items found while fixing and testing the 2026-10-17 audit items (PRs #48 to #56). Each section names the fixes it came from. None of these are regressions.

## Testing gaps

19. **The approval-prompt control-character warning was not tested manually.** The model removed raw ESC bytes from its tool calls. Covered by unit tests only.
20. **Bare `rm -rf /` forms were not run live.** The model refused them. They were tested as non-running forms (`false && ...`) and by calling the denylist check directly.

## Session files and streams (from the D1-D9 data loss fixes)

Minor items found while reviewing the D1-D9 fixes (plan: `docs/old/2026-10-17-data-loss-plan.md`). None of them lose data.

### Streams
33. **Tool entries stay "pending" after an early stream end.** Anthropic sends tool calls before `message_stop`, so the TUI shows them, but the turn is dropped. Mark pending entries as failed when `EventError` arrives. Cancelling mid-stream already did this before the fix.
34. **"stream ended early" is defined three times** (`anthropic/translate.go`, `openai/translate.go`, `agent/errors.go`). Use one sentinel in `common` so `errors.Is` works across packages.
35. **The `max_tokens`/`length` notice is not saved.** It is gone after a resume. `StopReason` is saved, so `historyToTimeline` could rebuild it.

### Testing gaps
37. **Weak assertions.** `stream_end_test.go` checks `err != nil` instead of the specific error.
38. **`TestPickResume_SkipsChildAndLoadsChildren` has no recorded failing run.** `TestListExcludesChildren` covers the same case and did fail first.

### Agent loop and TUI (found while fixing A1-A10)
41. **Sudo detection matches inside quoted strings.** `echo 'a; sudo b'` is treated as sudo. `VAR=/path/sudo cmd` (for example `SUDO=/usr/bin/sudo make install`) is also detected as sudo. The worst case is an extra password prompt. Fixing this needs a shell parser (`guard/sudo.go`).

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

