# Plan: Agent loop and TUI control flow (A1-A10)

Source: section 4 of `2026-10-17-codebase-audit.md`. The code was checked on `fix/data-loss-d1-d9` (`0ca2a6c`).

All work goes in one PR, with one commit per phase. Each fix starts with a failing test that reproduces the audit evidence, then the fix. TUI tests go through `Model.Update`, like the existing snapshot tests.

## Verification

| ID | Status | Notes |
|---|---|---|
| A1 | Fixed | #49 emits `EventError` for stream errors and a nil client (`turn.go:48-58`). Leftover: after a failed run, the user message stays in history with no reply. Moved to FOLLOWUP_TASKS. |
| A2 | Open | No `ctx.Err()` check between tool calls in `run.go`. `tui/frontend.go:89` sends the approval prompt before checking ctx. `cancelFocusedSessionWith` (`model.go:394`) does not clear `activeApproval` or `approvalQueue`. Run also continues after a cancelled compaction (FOLLOWUP #36). |
| A3 | Fixed by D5 | When the stream closes after a cancel, `Turn` returns `ctx.Err()` (`turn.go:78-85`). No work needed. |
| A4 | Open | All parts confirmed. Children share the parent's `SteerCh` (`spawn.go:84`). The queue is drained on any focused session's `RunDone`. `CancelSession` ignores `id`. A child's `RunDone` stops the global spinner. `ClearPending` drops a session switch, and the `ErrToolDenied` `continue` skips `DrainPending`. A late `RunDone` clears `InFlight` for the next run. |
| A5 | Open | `a.MaxTokens`, `ThinkingBudget` and `ReasoningEffort` are written with no lock and no defaults (`main.go:740`, `:813`). `Turn` and `Spawn` read `sess.Model` with no lock. |
| A6 | Open | `--resume` and `/resume` keep `sess.Model` from the JSONL but use the current client. |
| A7 | Open | All parts confirmed. |
| A8 | Open | The expanded approval prompt has no height cap or scroll (`approval.go:162`). |
| A9 | Open | All parts confirmed, including the trailing `\` swallowing `exit $?` in `wrapSudoPreauth`. |
| A10 | Open | All parts confirmed. |

## Decisions

1. **Cancelling a run needs a double Esc** (two presses within 500 ms, the same window as Ctrl+C). One press shows "Press Esc again to cancel". This prevents accidental cancels.
2. **Cancelling from a subagent view** cancels the whole run. Subagents run inside the main run's context, so per-child cancel would need a separate context per child.
3. **`/new` and `/resume` during a run** are refused with "cancel the run first".
4. **Model on resume:** the resumed session uses the current model config.
5. **Sudo in yolo mode** asks for the password only.
6. **One PR** covers all items.

## Changes during implementation

- **Prompt history uses Ctrl+P / Ctrl+N instead of Up.** Up on an empty editor is already timeline navigation, so it cannot also recall history.

## Phase 1: Cancel stops work (A2, FOLLOWUP #36)

### 1. Check ctx between tool calls
- `run.go`: before each tool call, check `ctx.Err()`. Remaining calls get a saved `skipped: cancelled` result (through `recordToolResult`), so history stays valid. Run then returns `ctx.Err()`.
- After compaction, return `ctx.Err()` if ctx was cancelled.
- Test: a batch of 2 whitelisted write calls with ctx cancelled after the first runs only the first, and both have results.

### 2. No approval prompt after cancel
- `tui/frontend.go` `RequestApproval`: return Deny with `ctx.Err()` before sending if ctx is already done.
- Test: a cancelled ctx sends no `ApprovalReqMsg`.

### 3. Clear approvals on cancel
- `cancelFocusedSessionWith`: reply Deny to `activeApproval` and every queued prompt, then clear both. Reply channels are buffered, so this does not block.
- Test: cancel with an active and a queued prompt leaves both cleared, and each reply channel received Deny.

## Phase 2: Steering, queue and session routing (A4)

### 4. Steers go to the main run only
- `Spawn` wraps the frontend for the child so `SteerCh()` returns nil. A steer typed during a subagent is picked up by the parent after the subagent returns.
- Subagent views are read-only: Enter in a depth-1 view shows "switch to the main session to send" and keeps the editor text.
- Test: a child agent never reads from the parent's steer channel.

### 5. Cancel targets the main run
- `CancelSession` keeps cancelling the main run. The TUI marks the main session and its in-flight children as cancelled, and adds "Cancelled." to the main session.
- Test: cancelling with a child focused clears `InFlight` on the main session and the child.

### 6. Spinner and queue follow the main session
- Only a depth-0 `RunDone` stops the spinner and drains the queue, whatever view is focused. The queued message is added to the main session's timeline.
- Test: a child `RunDone` while focused does not drain the queue or stop the spinner. A main `RunDone` while a child is focused drains the queue into the main session.

### 7. Late RunDone
- The TUI keeps a count of active runs for the main session: +1 when input is sent, -1 on `RunDone`. `InFlight` is true while the count is above 0. Cancel shows the session as idle right away, but the queue only drains when the count reaches 0.
- Test: cancel, send a new message, then a stale `RunDone` arrives: the session stays in flight.

### 8. Session switch during a run
- `/new` and `/resume` are refused while the main session is in flight.
- `runLoop`: call `DrainPending` before the `ErrToolDenied` `continue`, as a safety net.
- Fix the stale `Swap` comment in `state.go`.
- Test: `/new` while in flight leaves focus unchanged and shows the status message.

## Phase 3: Model switching (A5, A6)

### 9. Model settings behind the lock
- Move model name, max tokens, thinking budget and reasoning effort into `SessionState` behind its mutex, with a getter that returns them together. `Turn` and `Spawn` read through it.
- Test: `/model` during a running turn passes `go test -race`.

### 10. One helper applies a model config
- A helper in `main.go` applies a model config with the startup defaults (`DefaultMaxTokens`, `DefaultThinkingBudget`). Startup, `/model` and the setup wizard all use it.
- Test: switching to a config with no `maxTokens` gives `DefaultMaxTokens`, and thinking stays on.

### 11. Resume uses the current model
- After `--resume` and `/resume`, set the session's model and config name to the active config.
- Test: a resumed session with an old model name sends the current model name.

## Phase 4: Input handling (A7)

### 12. Esc closes the file popup first
- In `handleNormalKey`, handle Esc for the visible file popup before the spinner's Esc.
- Test: Esc with the popup open and a run in flight closes the popup and does not cancel.

### 13. Double Esc cancels a run
- The spinner's Esc handler cancels only when the previous Esc was within 500 ms. A single press sets the status "Press Esc again to cancel".
- The spinner hint changes from "(Esc to cancel)" to "(Esc Esc to cancel)". Update the snapshots.
- An Esc that closes the file popup does not count as the first press.
- Test: one Esc during a run does not cancel. Two within 500 ms cancel. Two more than 500 ms apart do not.

### 14. Only known commands are slash commands
- Treat input as a slash command only when the first word is in `slashCommands`. Anything else is sent as a normal message.
- Test: `/usr/bin/python3 crashes, why?` is sent as a message.

### 15. Arrow Down resumes auto-scroll
- After moving the cursor to the last entry, call `ReEnableAutoScrollIfAtBottom`. Fix the stale comment at `chat.go:94-95`.
- Test: scroll up, then Down to the last entry: auto-scroll is on.

### 16. Prompt history
- Add submitted text to the editor history from `model_keys.go`.
- `History.Prev` returns the draft when `pos < 0`.
- Up on the first row only opens history when the editor is empty or already browsing. Otherwise it does nothing.
- Test: submit two messages, Up twice recalls them in reverse order. Up on a non-empty draft keeps the cursor.

### 17. `/cancel` when idle
- Show "Nothing to cancel" and add no timeline entry.

## Phase 5: Approval prompt scrolling (A8)

### 18. Height cap and scroll
- Pass the terminal height to `renderApprovalOverlay`. When expanded, show only as many command lines as fit with the options, plus a scroll offset and a line-position hint.
- PgUp and PgDn scroll the command while a prompt is open.
- Test: a 42-line command at 80x20, expanded, shows `LINE_00`. PgDn shows later lines.

## Phase 6: Sudo edge cases (A9)

### 19. Locale-independent wrong password check
- `sudoRejected` also matches `tools.SudoPreauthFailedMarker`.

### 20. Windows
- `IsSudoCommand` is not used on Windows, so `NeedsSudoPassword` is never set there.

### 21. Yolo mode
- A sudo command in yolo mode goes through `askSudoPassword` instead of returning Allow right away.
- Test: yolo with `sudo ls` requests the password only.

### 22. More sudo forms
- Extend `sudoRe` for `VAR=x sudo`, `command sudo`, `exec sudo`, `nice sudo`, `nohup sudo`, `/usr/bin/sudo` and `\sudo`.
- Test: each form is detected. Existing negative cases still pass.

### 23. Trailing backslash
- `wrapSudoPreauth` adds a blank line after the command, so a trailing `\` cannot join `exit $?`.
- Test: `echo a \` with a sudo password returns the command's exit code.

The quoted-string false positive (`echo 'a; sudo b'`) needs a shell parser. Moved to FOLLOWUP_TASKS.

## Phase 7: Headless mode (A10)

### 24. Only the final main-session reply
- `Emit` ignores events from sessions other than the main one, and clears the text buffer on `EventSending`, so stdout gets only the final reply.
- Test: subagent text and intermediate turns do not reach stdout.

### 25. Ctrl+C while reading stdin
- Read stdin lines in a goroutine and select on ctx in `readStdin`.
- Test: cancelling ctx returns from `AwaitUserInput` while stdin is blocked.

### 26. Long stdin lines
- Read stdin with `bufio.Reader.ReadBytes('\n')`, as in D1.
- Test: a 70 KiB `user_message` line is read.

## Docs

- Mark A1-A10 in the audit.
- Add the A1 leftover (unanswered user message after a failed run) and the sudo quoting false positive to FOLLOWUP_TASKS. Remove FOLLOWUP #36 once fixed.
