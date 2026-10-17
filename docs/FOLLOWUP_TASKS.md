# Follow-up Tasks

Open items found while fixing and testing the 2026-10-17 audit items. The first sections come from the security fixes (PR #48); the last section comes from the D1-D9 data loss fixes. None of these are regressions.

## Denylist

1. **A project file can turn off the built-in denylist.** If there is no global `~/.gohome/denylist.json` (for example the user chose "No" in the setup wizard), a repo can ship `.gohome/denylist.json` with `{"shell":[]}`. The built-in rules are then dropped, and `rm -rf /` and `rm -rf ~` are not denied. Approval prompts still apply when yolo is off. Related to audit S2, which was marked as not an issue.
2. **Bypass forms that are not denied.** These still prompt when yolo is off:
   - `rm / -rf` (target before the flag)
   - `rm -rf //` and `rm -rf /.`
   - `bash -c "rm -rf /"` and `sh -c 'rm -rf ~'`
   - `find / -delete` and similar commands
3. **False positive.** `echo "; rm -rf /"` is denied, because the `;` inside the quotes is treated as a separator.
4. **Write and edit tools are not checked by the denylist.** Excluded from the security fixes plan.

## Whitelist and approval prompt

5. **"Allow always" on a chained command saves a rule that never matches.** For example `git status; echo x` suggests `^git status;`. Chained commands are never auto-approved, so the rule is useless. Offer no suggestion for commands with shell operators.
6. **Sudo commands with no suggestion cannot be saved.** Editing the pattern is disabled for sudo, so "Allow always" shows "pattern is empty". "Allow once" and "Deny" still work.
7. **No "(e edit)" hint when the pattern is empty,** and the `Error:` line stays visible while moving with the arrow keys.
8. **Old `^sudo` whitelist entries stay active.** Entries saved by the old suggestion still allow every sudo command (silently with the password cache on). Consider a warning in `Compile` or a migration.
9. **A tab is not a word boundary** in whitelist matching, so `ls<TAB>-la` prompts. Fails closed.
10. **`\r` is not treated as a shell operator** in `hasShellOperator`. Only matters for hand-written patterns, since commands with control characters get no suggestion.

## Terminal output

11. **Unicode bidi characters are not flagged.** Characters such as U+202E can visually reorder text in the approval prompt. Neither the sanitizer nor the control-character warning handles them.
12. **Unknown-tool summaries are cut to length before control characters are escaped,** so the cut point is computed on the raw text.
13. **Filenames in the file search list, session browser and `/resume` picker are not sanitized.**

## File permissions

14. **`whitelist.json` is created `0644`.**
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

Minor items found while reviewing the D1-D9 fixes (plan: `docs/reviews/2026-10-17-data-loss-plan.md`). None of them lose data.

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
39. **A symlinked working directory makes two session folders.** On macOS, `/var/...` and `/private/var/...` name the same directory but map to two session folders, so `/resume` from one path does not see sessions from the other. Resolve the cwd with `filepath.EvalSymlinks` before building the session path. Found during the live test of the D1-D9 fixes; this existed before.

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
52. **A file written by `write` must be read before `edit`.** `write` does not mark the file as read, so the model needs an extra `read` call.
53. **Read-before-edit does not follow symlinks.** Paths are compared in clean absolute form, but symlinks are not resolved. On macOS, reading `/tmp/x/a.txt` and then editing `/private/tmp/x/a.txt` returns "file must be read first". Related to item 39. Found during the live test of the T1-T3 fixes.

### TUI (deferred from the R1-R23 fixes)
54. **Multi-line paste on Windows submits only the first line (audit R5).** Bubble Tea v1.3.10's Windows console input never sets `Paste`, so each pasted Enter submits. Fix with Bubble Tea v2 or a timing heuristic. Needs testing on Windows.
55. **The editor works per rune, not per grapheme (audit R9).** ZWJ emoji and combining marks are split by cursor movement and Backspace. Needs a rewrite of the editor's cursor model.
56. **Streaming re-parses the whole entry on every frame (audit R22).** The render cache is keyed on the full text, so a streaming assistant entry is parsed and wrapped again for each delta. Needs incremental markdown rendering.

### Dependencies (deferred from the outdated components fixes)
57. **Bubble Tea, Lip Gloss and Bubbles v2 are out at `charm.land/...`.** gohome is on Bubble Tea v1.3.10, Lip Gloss v1.1.0 and Bubbles v1.0.0. Moving to v2 is a migration with API changes. It may also fix item 54 (Windows paste) and allow Shift+Enter.
