# Changelog

## Unreleased

Fixes for follow-up items 58-62 from the 2026-10-18 live test, file path fixes for items 39, 52 and 53, terminal output fixes for items 11, 12 and 13, whitelist and approval prompt fixes for items 5, 6, 7, 9, 10 and 14, setup wizard, logging and TUI fixes for items 16, 17, 18, 42, 44 and 65, session loading and writer fixes for items 21-29, subagent session fixes for items 30, 31 and 32, compaction and failed run fixes for items 40, 64 and 66, stream end fixes for items 33, 34, 35 and 37, retry and watchdog fixes for items 46 and 50, SSE and OpenAI wire fixes for items 47, 48 and 49, prompt caching for item 45, and wizard wrapping for item 67.

### Added

- **Guard debug logging** -- With `GOHOME_DEBUG=1`, every guard decision (whitelisted, denylisted, yolo, user choice) and every approval or sudo password prompt is written to the debug log with the session, tool and command. The sudo password is never logged (18).
- **`maxTokensField` model setting** -- Chooses `max_tokens` or `max_completion_tokens` for the OpenAI wire (47).
- **`promptCache` model setting** -- Opt-in Anthropic prompt caching (45).

### Fixed

- **Compaction summaries** -- The summary request now ends with a user message asking for the summary. Before, the request often ended with a tool call or tool result, and the model continued the conversation instead of summarizing it (58).
- **E2E compaction test** -- `TestE2EAutoCompact` raises `MaxTokens` to 4096 for the summary, because a reasoning model could spend the default 256 tokens on thinking and return no text. The test also checks that the summary is a summary (59).
- **Tabs on screen** -- Tabs in assistant markdown, code blocks and thinking entries are expanded to spaces. Raw tabs left old text, such as the spinner, visible until a full redraw (60).
- **Shell `cwd` errors** -- A missing `cwd` gives "cwd does not exist: <path>", and a file gives "cwd is not a folder: <path>", instead of `fork/exec /bin/sh: no such file or directory` (61).
- **Spinner during approval** -- The spinner says "Waiting for approval..." while the approval menu is open (62).
- **Edit after write** -- A file created or overwritten with `write` counts as read, so `edit` no longer needs a `read` first (52).
- **Read-before-edit follows symlinks** -- Reading a file through a symlink and editing it through its real path (or the other way round) now works. On macOS, `/tmp/x/a.txt` and `/private/tmp/x/a.txt` match (53).
- **One session folder per directory** -- Symlinks in the working directory are resolved before the session folder name is built, so `/resume` from `/tmp/x` and from `/private/tmp/x` finds the same sessions. Sessions saved earlier from a path that contains a symlink were stored under the unresolved path and no longer show in `/resume`. Paths without symlinks are not affected (39).
- **Bidi characters** -- Bidi controls (U+061C, U+200E, U+200F, U+202A-U+202E, U+2066-U+2069) can make a command look different from what runs. The approval prompt now shows them as `\uNNNN` with the control-character warning, timeline text drops them, and a command with one gets no suggested whitelist pattern (11).
- **Unknown-tool summaries** -- The 60-column cut is now made after control characters are removed (timeline) or written out (approval prompt), so it can no longer split an escape sequence (12).
- **File names and session titles** -- Control sequences are removed from file names in the file search list and from session titles in the `/resume` picker. The selected file path is unchanged (13).
- **No pattern for chained commands** -- A command with a shell operator (`;`, `|`, `&`, `$(`, redirects, newlines) gets no suggested whitelist pattern. Such commands are never auto-approved, so a rule like `^git status;` never matched (5).
- **Allow always on sudo** -- A sudo command with no suggested pattern shows "Allow always (not available for this command)" instead of failing with "pattern is empty". Sudo prompts no longer show the "(e edit)" hint, since editing is disabled for sudo (6).
- **Empty-pattern hint** -- "Allow always" shows "(e edit)" when there is no suggested pattern, and the error line clears when moving with the arrow keys (7).
- **Tabs in whitelist matching** -- A tab now counts as a word boundary, so `^ls` allows `ls<TAB>-la` (9).
- **Carriage return** -- `\r` counts as a shell operator, so a command with one is never auto-approved (10).
- **Whitelist file mode** -- A new project `whitelist.json` is created `0600` and its folder `0700`. Existing files keep their mode (14).
- **Session write errors at exit** -- A session write or close error is printed to stderr ("gohome: session not saved") when gohome exits, in both TUI and headless mode. Before, a failure on the last turn was never reported, and headless mode never reported one (26, 28).
- **No broken lines after a failed write** -- Each session line and its newline are written in one call, so a failed write no longer leaves a lone newline (27).
- **Late session events** -- An event emitted after the session file is closed is logged at Warn instead of Debug (29).
- **Repaired tool results** -- Synthetic "skipped: no result recorded" results are now in `tool_use` order with the recorded ones (24).
- **Session loading** -- Lines are decoded without an extra copy, and blank-session cleanup stops reading a file at the first user message (21, 22).
- **Subagent IDs after a crash** -- A new subagent also skips child IDs already used by subagent session files on disk. Before, a crash between the child's start and the parent's spawn marker let the next resume reuse `<parentID>-sub-N` (30).
- **Resumed subagent links** -- A child session now records the tool_use ID of the parent's subagent call, and `/resume` links the child to that entry. Two subagents with the same task no longer swap. Task matching and newest-to-oldest linking are kept for children without a unique ID match (31).
- **Task matching with invalid UTF-8** -- Resumed children are matched on the raw task from the history instead of the cleaned timeline text, so a task with invalid UTF-8 still finds its entry (32).
- **Denylist confirmation** -- After creating `denylist.json`, the setup wizard shows "Wrote <path>" as step 10/10 before it closes. Choosing No closes the wizard as before (16).
- **Wizard text wrapping** -- The wizard prompt and error line wrap to the terminal width (17).
- **Sudo dialog scrolling** -- The full command shown with Tab in the sudo password dialog is capped to the screen height and scrolls with PgUp/PgDn, like the approval menu (42).
- **Model on resume** -- A resumed session that runs with a different model writes a `model_change` record, so the session file shows the model the requests use. `/model` switches are recorded the same way (44).
- **Control characters in the editor** -- The editor, live user entries and queued messages remove control sequences and bidi controls when drawn. A file name with an escape sequence picked from the file search list, or a pasted ESC or U+202E, no longer reaches the terminal. The text sent to the model is unchanged (65).
- **Rejected compaction summaries** -- A summary is not applied when it stopped at the output token limit (`max_tokens` or `length`), is empty, or is not shorter than the messages it replaces. The full history is kept and a notice says why (64, 66).
- **Fewer repeated compactions** -- After a rejected summary, compaction waits until the messages it kept as recent are part of what gets summarized, instead of asking again on every turn (66).
- **Failed runs are marked** -- When a run fails (not cancelled), an assistant message "[Run failed] <error>" is added after the unanswered message and saved in the session file. It shows on resume, the model sees it on the next turn, and roles still alternate (40).
- **Stream ended early** -- Both wires return one `ErrStreamEndedEarly` sentinel, so `errors.Is` works across packages (34, 37).
- **Pending tools after a stream error** -- Pending tool entries are marked as failed when a stream error arrives (33).
- **Token limit notice on resume** -- The `max_tokens`/`length` notice is rebuilt when a session is resumed (35).
- **Overload retry** -- A mid-stream `overloaded_error` is retried when no content has been received yet (46).
- **Idle watchdog** -- The idle-read watchdog pauses while the stream consumer is slow, so a slow TUI or stdout no longer cuts the stream (50).
- **SSE parsing** -- One SSE parser serves both wires and handles CR and CRLF line endings and multi-line `data:` payloads (49).
- **OpenAI tool call ids** -- A new tool call id at an index already in use starts a new tool call instead of joining the arguments (48).
- **Wizard wrapping** -- Wizard option descriptions and notices wrap at narrow widths (67).

## v0.4.4-dev2

Fixes from the 2026-10-17 codebase audit. IDs such as S1 or D4 refer to that report.

### Added

- **`thinking` model setting** -- `off`, `adaptive` or `budget` for the Anthropic wire. When unset, gohome uses `budget` if `thinkingBudget > 0`, otherwise `adaptive`. Adaptive sends `{"type":"adaptive","display":"summarized"}`. The budget must be at least 1024 and below `maxTokens`, checked at startup and on `/model` (L1, #52).
- **`reasoningEffort` on the Anthropic wire** -- Now sent as `output_config.effort` (L1, #52).
- **Retry-After and jitter** -- Retries now cover 408, 409, 429, 5xx and connection errors, honor `Retry-After` (capped at 60 s) and add jitter (L4, #52).
- **HTTP timeouts** -- A 5-minute response-header timeout and a 5-minute idle-read watchdog. There is no total client timeout, so long streams still work (L4, #52).
- **Prompt history** -- `Ctrl+P` / `Ctrl+N` recall earlier prompts and restore the draft (A7, #51).
- **Scrollable approval prompt** -- The expanded approval prompt has a height cap and scrolls with PgUp/PgDn (A8, #51).
- **Full command in the sudo password dialog** -- `Tab` shows the whole command before you enter the password (S4, #48).
- **Control characters shown in approval prompts** -- They appear as `\xNN` with a warning line (S5, #48).
- **Resumed subagents** -- Resuming a session loads its subagent sessions as read-only views linked to their tool entries (D2, #50).
- **"session not saved" notice** -- Session file write errors are shown in the TUI (D8, #50).
- **Truncation notice** -- A `max_tokens` or `length` stop reason shows a notice (D5, #50).
- **Config overlay scrolling** -- The `/config` overlay is clipped to the screen and scrolls with PgUp/PgDn (R20, #54).
- **Restart notice after `/config`** -- Saving the config says to restart gohome, and setup wizard failures show a notice (H5, #49).

### Changed

- **Default thinking mode is now adaptive** -- A model config with no `thinkingBudget` used to send a 10240 budget. It now sends adaptive thinking. Older models or proxies without adaptive support need `"thinking": "budget"` or `"thinking": "off"` (L1, #52).
- **Invalid settings JSON is an error** -- gohome exits and prints the file path instead of treating the file as empty (L7, #52).
- **Error bodies are capped** -- HTTP error bodies are read up to 64 KiB (L4, #52).
- **Esc Esc cancels a run** -- One `Esc` shows "Press Esc again to cancel", and `Esc` closes the file popup first (A7, #51).
- **External editor moved to `Ctrl+G`** -- `Ctrl+E` now moves to the end of the input line (R18, #54).
- **Session delete in the `/resume` browser** -- Delete is `Ctrl+D` then `y` (or `Ctrl+D` twice), and `d` types into the filter (R17, #54).
- **Default denylist rules** -- New setups get regex rules for `rm` and `mkfs` that handle flag order, quotes, `~`, `$HOME` and wrappers such as `sudo` and `env`. Spaces are squeezed before matching (S7, #48).
- **Owner-only config files** -- New settings and denylist files are written `0600`, and new `~/.gohome` folders are created `0700` (S6, #48).
- **Subagent views are read-only** -- Steers go to the main run, and cancel always targets the main run (A4, #51).
- **`/new` and `/resume` are refused during a run** -- Cancel the run first (A4, #51).
- **Resume uses the current model config** -- A resumed session no longer sends the model name stored in the session file (A6, #51).
- **Headless output** -- `--prompt` prints only the final main-session reply (A10, #51).
- **Sudo in yolo mode** -- Yolo opens the password dialog for sudo commands. Windows no longer shows a sudo dialog (A9, #51).
- **Subagent session IDs** -- Child sessions are named `<parentID>-sub-N` and are hidden from resume lists (D2, #50).
- **Shell output cap** -- Shell output keeps the first and last 32 KiB with an omitted-bytes marker (T1, #53).
- **Read tool limits** -- Lines over 2000 characters are cut, and a read stops at 100 KiB with a note giving the offset to continue from (T1, #53).
- **Shell timeouts** -- `timeout_ms` of 0 or less uses the default, and the tool description shows the configured timeouts (T3, #53).
- **Thinking entries collapse** -- `Enter` collapses and expands thinking entries. They are expanded by default (#54).
- **Go 1.26.9** -- `go.mod` and both workflows move from Go 1.25, which is no longer supported. This fixes 15 reachable standard library vulnerabilities reported by govulncheck (#55).
- **Dependencies updated** -- `golang.org/x/sys` v0.49.0 (fixes GO-2026-5024), chroma v2.27.0, goldmark v1.8.6, `x/exp/golden` v0.1.0 and a newer `teatest` (#55).
- **CI and release workflows** -- GitHub Actions moved to their latest major versions, golangci-lint is pinned to v2.15.0, CI builds set `main.version`, and every built binary has the 25 MB size check (#55).
- **Release notes** -- A release uses only its own CHANGELOG section. The release fails if the section is missing (#55).

### Fixed

- **`sudo -n` and pipes into sudo failed** -- The password is checked with `sudo -v` and passed on a separate file descriptor instead of stdin. If the check fails, the command does not run (M5, #47).
- **Whitelisted commands could be chained** -- Commands with shell operators never auto-approve, patterns must match whole words, and alternations stay anchored (S1, #48).
- **"Allow always" could whitelist everything** -- Empty and invalid patterns are rejected, suggestions are regex-escaped, and sudo suggestions are the full command ending in `$` (S3, #48).
- **Terminal escape sequences reached the terminal** -- Agent text, tool output, tool names, diffs and resumed history are sanitized (S5, #48).
- **Settings save errors were hidden** -- The setup wizard stays on the current step and shows the error (S6, #48).
- **Shell tool hung on output lines over 1 MiB** -- The pipe is drained after a scanner error (H1, #49).
- **Shell timeout left child processes running** -- The process group is killed, and partial output and the exit code are kept (H2, #49).
- **TUI froze or crashed at small terminal widths** -- Wrapping and horizontal rules handle widths of 0 or less (H3, H4, #49).
- **Cancelling the setup wizard crashed on the first message** -- A missing client is now an error (H5, #49).
- **HTTP and stream errors were not shown in the TUI** -- They are now shown as errors (A1, #49).
- **Sessions with a line over 64 KiB could not be loaded or listed** -- Long lines now load (D1, #50).
- **Tool results skipped by a steer were not saved** -- They are saved, and a `tool_use` with no result is repaired on load (D3, #50).
- **Invalid tool-call JSON broke later Anthropic requests** -- Invalid input is stored as `{}` and the call returns an error result (D4, #50).
- **Streams that ended early were treated as complete** -- They are now errors on both wires (D5, A3, #50).
- **Compacting a resumed session could orphan tool results** -- Replayed tool results are grouped into one message (D6, #50).
- **Cancelling during compaction saved a cut-off summary** -- The summary is applied only after the turn completes (D7, #50).
- **Emit after Close panicked** -- The event is dropped instead (D9, #50).
- **Work continued after cancel** -- Remaining tool calls are skipped, and active and queued approvals are denied and cleared (A2, #51).
- **Steer, queue and run routing** -- Only the main session's run end stops the spinner and drains the queue, and a late run end from a cancelled run is ignored (A4, #51).
- **`/model` raced with the running turn** -- Model settings are behind a lock, and switching applies the startup defaults (A5, #51).
- **Input handling** -- Only known commands are treated as slash commands, Arrow Down resumes auto-scroll, and `/cancel` when idle says "Nothing to cancel" (A7, #51).
- **Sudo edge cases** -- Wrong-password detection works in non-English locales, more sudo forms are detected, and a trailing `\` no longer swallows the exit code (A9, #51).
- **Headless mode** -- `Ctrl+C` works while reading stdin, and long stdin lines work (A10, #51).
- **Edit tool bugs** -- An empty `old_string` is rejected, and on CRLF files an LF-only `old_string` is retried with CRLF line endings (T2, #53).
- **Read-before-edit compared raw paths** -- Paths are compared in clean absolute form (T3, #53).
- **Styling lost at wrap points** -- The full SGR state is kept across wraps, and words that fit on the next line are not broken (R1, R12, #54).
- **Markdown rendering** -- Raw HTML is shown as text, code blocks and headings wrap, nested lists are indented once per level, wide tables fit, and heading style survives emphasis (R2, R3, R13, #54).
- **Tool entries** -- Diff colors use the marker position, tabs expand by column, multi-line commands show their first line, and tool args use `path` and `task` (R6, R11, R14, R15, #54).
- **File search** -- The list scrolls with the selection, results are scored before the cap, `fd` gets `--`, and the `find` fallback skips `.git/` (R7, R8, #54).
- **Backspace removed one byte** -- It now removes one character (R16, #54).
- **`/resume` could delete the active session** -- It is refused, and delete errors are shown (R19, #54).
- **Highlight cache churn and wheel timers** -- The cache evicts one entry at a time, and the mouse wheel uses one idle timer (R21, R23, #54).
- **Thinking blocks were merged** -- Thinking blocks keep arrival order and their own signatures, and signature-only blocks are kept (L2, #52).
- **`redacted_thinking` blocks were dropped** -- They are read, saved and sent back (L3, #52).
- **OpenAI error chunks ended the turn normally** -- `{"error":...}` chunks are now errors (L5, #52).
- **OpenAI wire details** -- `delta.reasoning` is read, a thinking-only assistant turn sends `content: ""`, and user text blocks are joined with a newline (L8, #52).
- **Config skeleton wrote placeholders** -- The skeleton no longer has an `example` model or `"string"` values (L6, #52).
- **Project files could not turn settings off** -- A project file can set `autoCompact` and `cacheSudoPassword` to false (L7, #52).
- **`retryBackoffMs` had no effect** -- The clients now use it (L7, #52).
- **Compaction ignored cache tokens** -- Compaction counts cache-read and cache-write tokens (L9, #52).
- **Windows release binaries were stripped and cross-compiled** -- The release workflow builds Windows natively on `windows-latest` without `-s -w`, matching CI (#55).
- **Docs and in-app help were out of date** -- README, help overlay, AGENTS.md and FUTURE.md now match the code (#56).

### Removed

- **`autoCompactTargetPct`** -- It was never read (L7, #52).

## v0.4.4-dev1

### Added

- **Sudo password dialog** -- After choosing **Allow once** or **Allow always** on a sudo command, a separate password dialog opens. Every key goes to the password field; Enter runs the command, Esc goes back to the menu, and Ctrl+C denies. The dialog also shows for subagent sudo commands (#45).
- **`cacheSudoPassword` setting** -- Opt-in setting (default `false`) that keeps the sudo password in memory until `gohome` exits. If sudo rejects the cached password, the cache is cleared and the next sudo command asks again. The setup wizard offers this option (#45).

### Fixed

- **Password keys triggered approval menu shortcuts** -- Sudo approval is now two steps, so typing the password can no longer select menu options such as allow or deny (#45).
- **"Allow always" on a sudo command broke later sudo runs** -- Whitelisted sudo commands now skip the menu but still open the password dialog, instead of running without a password (M4, #45).
- **Terminal prompts drew over the TUI** -- Shell commands now run in a new session with no controlling terminal on Unix, so programs that prompt on the terminal (sudo without a password, ssh password prompts) fail instead of taking over the screen (#45).
- **Approval and sudo dialog text overflowed** -- Text now wraps to the real content width (#45).
- **Mouse wheel moved the approval menu** -- Mouse capture stays on during approvals, so the wheel scrolls the chat; hold Shift to select text (#45).

## v0.4.3

### Added

- **`Ctrl+Y` copies the selected timeline entry** -- Replaces the old `c` key binding for copying the entry under the timeline cursor (H6, #42).
### Fixed

- **Steering corrupted multi-tool turns** -- A steer arriving between tool calls now produces "skipped: user steered" results for the remaining tools, and the steer message is appended after the complete tool result message. Previously `tool_use` blocks were left without matching `tool_result` blocks, which made the history invalid (H1, #42).
- **Steer messages delayed on text-only turns** -- When the LLM replies without tool calls, pending steers are drained and the loop continues. Stale steers are also drained on cancel so they do not leak into the next run (H2, #42).
- **Headless `--prompt -` auto-approved tools** -- `--prompt -` now requires `--yolo`, the same as `--prompt "text"` (H3, #42).
- **Auto-compaction requests rejected by the Anthropic API** -- Tool blocks are converted to plain text before summarization, `MaxTokens` defaults to 4096 when unset, and the split point no longer separates a `tool_use` from its result (H4, #42).
- **Inconsistent history after resuming a compacted session** -- The `Compaction` event now stores the full post-compaction history, and `Load()` restores it. Older session files fall back to the summary-only behavior (H5, #42).
- **`c` key swallowed when the editor was empty** -- Typing `c` into an empty editor now inserts the character; copy moved to `Ctrl+Y` (H6, #42).
- **Spinner tick chains multiplied** -- Only one 80 ms tick chain runs at a time. Previously every token delta started a new chain, so redraws grew with response length (#43).
- **Render throttle had no effect** -- Throttled deltas now reuse the last rendered frame instead of only deferring `rebuildViewport`. `renderThrottleMs: 0` now applies the 16 ms default (L2, #43).

### Changed

- **Chat render caching** -- Entry renders are cached whenever they are computed, so streaming entries are no longer rendered twice per frame and offscreen entries stay cached after resume or resize. The cursor marker is applied at output time, so moving the cursor no longer invalidates caches (#43).
- **Syntax highlighting cached** -- Highlighted code blocks are cached by language and code, and the chroma style and formatter are resolved once (#43).
- **`renderThrottleMs` semantics** -- `0` means the 16 ms default; a negative value redraws on every token (#43).

## v0.4.2

### Added

- **Headless CLI mode (`--prompt`)** -- Non-interactive execution. `--prompt "text"` runs a single prompt headlessly (requires `--yolo`), prints the final assistant text, and exits. `--prompt -` enables interactive JSONL-over-stdin/stdout mode for programmatic multi-turn sessions (requires `--verbose`). New `--verbose` flag emits all agent events as JSON lines. New `internal/headless` package implements `agent.Frontend` (#38).
- **Denylist guard** -- Shell command denylist that rejects dangerous commands before yolo or whitelist checks. Default patterns block `rm -rf /`, `mkfs`, fork bombs, and writes to `/dev/sd*`. Users can override via `~/.gohome/denylist.json` and `.gohome/denylist.json`. Supports both substring and regex matching (prefixed with `regex:`). Denylist rejections return a `DenyInfo` message to the LLM so it can self-correct. Config wizard now offers optional denylist scaffolding (#38).
- **Mid-turn steering** -- Users can now inject messages between tool calls while the agent is in-flight, instead of waiting for all tools to complete. Steered messages appear as user entries in the timeline. New `EventRunDone` event distinguishes "turn done" from "run done", fixing the InFlight dead zone (#40).
- **Mouse idle timeout** -- After 2 seconds of no mouse wheel activity, mouse capture is automatically disabled so native text selection works. The next scroll re-enables it. Clicking shows a transient "Hold Shift to select text" hint in the status bar (#40).
- **`/mouse` slash command** -- Explicit toggle for mouse tracking on/off (#40).
- **`/clear-queue` slash command** -- Clears all pending queued messages (#40).
- **`Ctrl+D` to pop last queued message** -- Removes the most recently queued pending message (#40).
- **Pending message combining** -- When the agent finishes a run and pending messages exist, they are combined into a single message (joined with newlines) instead of firing one at a time (#40).
- **Approval prompt expand/collapse** -- Long approval summaries are now truncated to 3 lines with a "(v to expand)" hint. Press `v` to toggle full view (#40).
- **E2E test suite** -- 11 end-to-end tests against live LLM endpoints covering smoke roundtrip, headless tool call, shell tool, write+read chain, edit tool, error recovery, subagent spawn, session resume, headless multi-turn, interactive with tools, and auto-compaction. Runs with `-tags e2e` build tag (never in CI) (#39).
- **Windows Defender mitigations** -- PE application manifest embedded for UAC level, DPI awareness, and UTF-8 codepage. Version info embedded via `goversioninfo`. Windows binary now built natively on `windows-latest` CI runner instead of cross-compiled. Strip flags (`-s -w`) no longer used for Windows builds (#39).

### Fixed

- **Tool results matched to wrong entries** -- When multiple tool calls arrived in a single LLM turn, results were matched by backwards position search, causing tool A's output to display under tool B's entry. Now stores `ToolUseID` on `TimelineEntry` and matches by ID (#40).
- **Auto-scroll re-enable** -- Scrolling down to the bottom of the conversation now re-enables auto-scroll. Previously `ScrollDown` unconditionally disabled auto-scroll (#40).
- **Denylist rejection halted agent** -- Denylist rejections now return `DenyInfo` to the LLM (as a tool error result) instead of halting the agent, allowing it to self-correct (#38).
- **Partial compaction for cache preservation** -- Auto-compact now keeps the first 2 messages (stable prefix) and last 4 messages (recent turns) intact, only summarizing the middle portion. This preserves the prompt cache prefix and recent context (#40).
- **Config wizard unreachable** -- Model resolution exited with `os.Exit(1)` when `ModelConfig` was empty, making the setup wizard unreachable. Now defers model resolution so the wizard can trigger in interactive mode (#38).

### Changed

- **Chat rendering optimized** -- `Render()` now uses a two-pass approach: pass 1 computes line offsets via `entryLineCount` (cache-aware), pass 2 only renders entries overlapping the visible window. Skips offscreen entries entirely (#40).
- **`NewGuard` signature** -- Now accepts a `*Denylist` parameter (nil disables deny checking): `NewGuard(wl, fe, dl)` (#38).
- **`agent.Frontend` interface** -- Added `SteerCh() <-chan string` method (#40).
- **CI: Windows build now native** -- Windows binary is built on `windows-latest` runner instead of cross-compiled from Linux (#39).

## v0.4.1

### Added

- **Auto-compaction** -- Automatic conversation summarization when token usage approaches the context window limit. Configurable via `autoCompact`, `autoCompactMode`, `autoCompactPct`, and `autoCompactLeftover` settings. Supports percentage and leftover trigger modes (#35).
- **Sudo password handling** -- Shell commands requiring sudo now trigger a dedicated approval prompt with password input, forwarded to the process via stdin (#36).
- **Smart auto-scroll** -- Viewport no longer forces scroll-to-bottom on every update. Manual scroll position is preserved; auto-scroll resumes only on user submit, slash commands, session switching, or cancel (#36).
- **PgUp/PgDown during approval prompts** -- Timeline scrolling now works while an approval prompt is active (#36).
- **Mouse wheel scrolling** -- Mouse wheel events scroll the timeline viewport (#36).

### Changed

- **Shell tool (renamed from bash)** -- The tool exposed to the LLM is now named "shell". On Windows, the executor launches PowerShell instead of cmd /c, so the LLM generates native commands. Config fields renamed from `bashTimeoutMs`/`maxBashTimeoutMs` to `shellTimeoutMs`/`maxShellTimeoutMs` (#34).

## v0.4.0

### Added

- **Unified FIFO approval queue** -- Subagent tool approvals now surface in the main session without requiring session switching. A flat FIFO queue replaces the per-session pending map, with session labels on cross-session approval overlays (#30).
- **Denial return-to-user flow** -- Tool denial now returns control to the user instead of immediately sending the denial back to the LLM. New `ErrToolDenied` sentinel and `EventToolDenied` event, with a "Tool call denied" notice in the TUI (#31).
- **TUI spinner states** -- Spinner now transitions through Sending, Thinking, and Processing states for clearer feedback during LLM turns (#32).

### Fixed

- **Token tracking** -- Token counts are now replaced per-turn instead of accumulated, matching how Anthropic and OpenAI report total input tokens (#32).
- **OpenAI adapter usage capture** -- Usage data is now captured from non-empty-choices chunks, fixing missing token counts for OpenAI-wire endpoints (#32).
- **CWD in statusbar** -- The current working directory and git branch are now shown in the status bar (#32).
- **Windows clipboard null-byte stripping** -- Null bytes from Windows clipboard paste are now stripped (#28).
- **Scroll line counter** -- Scroll position now tracks cursor position correctly and reaches 100% (#28).

### Changed

- **Architecture: restored in-process** -- Removed the daemon-client model introduced in v0.3.0. The agent now runs in the same process as the TUI with direct channel communication, eliminating Unix socket dependency, RPC serialization overhead, and associated concurrency bugs. Windows support is restored (#29).
- **In-process model switching** -- /model command rebuilds the LLM client directly in-process instead of routing through daemon RPC. Agent params (maxTokens, thinkingBudget, reasoningEffort) are updated immediately.

### Removed

- **Daemon mode** -- Removed daemon server, JSON-RPC protocol, Unix socket communication, RPCFrontend, ClientFrontend, and --stop CLI flag (~3,085 LOC removed, net -4,127 LOC) (#29).
- **--stop CLI flag** -- No daemon to stop.

## v0.2.5

### Added

- **Subagent shadow entries** -- Child subagent tool calls are mirrored as indented, dimmed entries in the parent timeline, providing visibility into subagent activity without switching focus. A sliding window of max 3 shadows per child is maintained.
- **Tool output preview** -- Collapsed tool entries now show the last 3 lines of output as dimmed preview text, giving a quick glance at results without expanding.
- **Edit tool diff box** -- Edit tool calls render a bordered unified diff preview below the entry, with red/green line coloring for removals/additions. Error status shows a red border with dimmed content.
- **Subagent read-only mode** -- Completed subagent sessions now display a "[Session complete]" label and reject text input, preventing accidental interaction with finished sessions.
- **Cursor-viewport sync** -- Arrow key navigation through timeline entries now scrolls the viewport to keep the cursor on-screen, with pinning for entries taller than the viewport.

### Fixed

- **Subagent sessions stuck in "running" state** -- `EventSessionEnded` is now emitted when a subagent finishes or is cancelled, so the TUI correctly clears the InFlight flag (#16).
- **Subagent lifecycle state tracking** -- Subagent sessions now show accurate states: "running" while InFlight, "returning" when returning results to the parent, and "complete" when the parent receives the tool result (#18).
- **Scroll flicker on tall entries** -- `EnsureCursorVisible` no longer oscillates between top and bottom when an entry is taller than the viewport.
- **PgUp/PgDn scroll amount** -- Page scrolling now uses full-page increments instead of a fixed 5-line jump.
- **WrapText newline handling** -- Fixed line counting for content containing embedded newlines, correcting scroll math for multi-line tool output.

### Changed

- **LLM client scaffold deduplication** -- Extracted shared HTTP retry, error handling, and SSE pump logic into `common.StreamRequest`, eliminating ~165 lines of duplicated code between the Anthropic and OpenAI adapters. Deleted per-adapter `retry.go` files (M1).

## v0.2.4

### Fixed

- **Mutex self-deadlock in session swaps** -- `/new` and `/resume` slash command closures no longer deadlock by re-acquiring a held lock; closures now receive `(oldSess, oldWriter)` as parameters.
- **Data race in model switching** -- `SetModel` callback no longer writes `sess.ModelConfig` outside the lock; new `SetModelConfig()` method provides proper synchronization.
- **Stale model state on `/new`** -- `/new` now reads the current model config at call time instead of capturing the startup value.
- **Crash on `/new` after `/resume`** -- `runLoop` held stale session/writer references across the blocking `AwaitUserInput` call; a session swap closed the old writer, and sending on the closed channel panicked. References are now re-fetched after unblocking.

### Changed

- **Active modal system** -- Replaced the cascading flag-based modal system with an `activeModal` slot implementing the `Interactive` interface. New `HelpOverlay` and `TokensOverlay` types reduce `Update()` key dispatch from a 6-level priority ladder to a 3-level cascade (H4 completion).
- **Consolidated `/endpoint` and `/model`** -- Merged the half-implemented `/endpoint` and `/model` commands into a single `/model` command that rebuilds the LLM client at runtime. Config terminology renamed from `Endpoint` to `ModelConfig` throughout (M2).
- **File reorganization** -- Renamed `model_overlays.go` to `model_external_editor.go` (now contains only external editor logic).

## v0.2.3

### Added

- **Render caching** -- Per-entry render cache eliminates redundant markdown and syntax highlighting on unchanged timeline entries, dramatically improving TUI responsiveness in large conversations.
- **Render throttling** -- New optional `renderThrottleMs` config setting gates redraws during token streaming, preventing excessive refreshes on slow terminals. Defaults to 0 (no throttling).
- **Editor word wrap** -- Long lines in the input editor now wrap at word boundaries with sticky column navigation.
- **CLAUDE.md developer guide** -- Comprehensive documentation of project architecture, build/test commands, key interfaces, data flow, CI setup, and debug logging.
- **Code audit (FABLE_REVIEW.md)** -- Detailed technical review documenting high, medium, and low priority findings with line-number citations and suggested fixes.

### Fixed

- **Turn cancellation wired end-to-end** -- `/cancel`, Esc, and single Ctrl+C now properly cancel in-flight LLM turns via per-turn context cancellation, preventing UI/agent state divergence (H1).
- **Data races in session swaps** -- `/new` and `/resume` are now safe to call mid-turn. A new mutex-guarded `agent.State` type owns session and writer references, and swaps cancel the active turn first (H2).
- **OpenAI adapter parallel tool results** -- Multiple tool results in a single turn are no longer silently dropped. `buildOpenAIBody` now flattens `RoleTool` messages into one `openaiToolMessage` per `BlockToolResult` block (H3).

### Changed

- **TUI model decomposition** -- Split `model.go` (1407 lines) into focused subfiles: `model_agent.go`, `model_approval.go`, `model_keys.go`, `model_slash.go`, `model_overlays.go` (partial H4).
- **Main loop refactored** -- New `runLoop()` encapsulates the agent REPL with per-turn context cancellation, cleanly separating the event loop from session lifecycle management.

## v0.2.2

### Added

- **Help overlay** -- `/help` command and `Ctrl+H` keybinding open a scrollable overlay showing all keybindings, slash commands, and CLI flags.
- **Configurable settings** -- Context window, max tokens, thinking budget, bash timeouts, context warning thresholds, and retry backoff are now configurable via `settings.json` instead of being hardcoded. All new fields use `omitempty` so existing configs require no changes.
- **Copy to clipboard** -- Press `c` while browsing history to copy the selected entry to the system clipboard. ( Does not work over SSH )
- **Scroll-stable block expansion** -- Expanding thinking or tool blocks no longer jumps the viewport. The scroll position is anchored and expanded blocks get a subtle background highlight.
- **Thinking blocks in OpenAI wire protocol** -- The OpenAI-compatible adapter now parses `reasoning_content` deltas and emits thinking events, enabling thinking block rendering for OpenAI-wire endpoints.
- **Thinking block persistence** -- Thinking blocks (including signature data) are saved to session JSONL files so they are preserved and displayed when resuming a session.
- **Session load validation** -- Malformed thinking blocks (empty text) are logged as warnings when loading session files, providing visibility into data quality without blocking resume.

### Fixed

- Thinking block signature field is now captured from Anthropic `signature_delta` events and forwarded through the stream, fixing extended thinking continuations on session resume.
- LLM adapters now handle `BlockThinking` when building request messages for resumed sessions, preventing dropped thinking blocks.
- The `c` key no longer swallows input when the timeline is empty.
- Context warning thresholds are validated at startup to ensure the warning level is below the critical level.
- Thinking blocks now collapse automatically after reasoning completes, matching the collapsed-by-default behavior of resumed sessions.
- Resumed thinking blocks default to collapsed (previously expanded), reducing visual noise when loading old sessions.

## v0.2.1

### Added

- **Thinking blocks** -- Anthropic thinking/reasoning blocks are parsed from the SSE stream, forwarded through the agent event pipeline, and rendered in the TUI as collapsible entries with line counts. Spinner shows "Thinking..." during reasoning and "Generating..." during token output.
- **File search popup** -- Type `@` followed by a query to search project files using `fd` (with `find` fallback). Results are scored and ranked (exact filename > prefix > substring). Navigate with Up/Down or Tab, confirm with Enter to insert the path.
- **Pending message queue** -- Messages typed while the agent is streaming are queued and automatically sent when the current turn completes. Queue is cleared on `/cancel`.
- **Ctrl+C cancellation** -- Ctrl+C cancels in-flight turns or dismisses approval prompts; double-tap quits the app. Escape also cancels the spinner.
- **Table rendering** -- GFM tables are rendered with box-drawing borders in markdown output.
- **Tab completion for slash commands** -- Tab auto-completes slash commands with first-match highlighting in the palette.
- **Session browser** -- `/resume` now shows an interactive session browser with filtering and search via a new SelectListComponent.
- **Model selector** -- New `/model` command with interactive ModelSelectorComponent for switching LLM models at runtime.
- **Resume with history** -- When resuming a session, prior conversation history is loaded and displayed in the TUI.
- **Approval prompt navigation** -- Up/Down arrows navigate approval options with selection markers, Enter confirms. Press `e` to edit allow-always patterns, `4` for deny+steer with steering message input.
- **Blank session cleanup** -- Empty JSONL session files (no user messages) are automatically removed on shutdown.
- **Token usage overlay** -- `/tokens` overlay shows token usage breakdown and context window percentage.
- **Context fullness warnings** -- Warnings displayed at 80% and 95% context window thresholds.
- **Tool status colors** -- Tool execution results shown with pending/success/error color indicators.
- **Status bar** -- Session ID, model name, token progress bar, and YOLO mode indicator.

### Fixed

- CI release workflow now creates draft releases and handles pre-existing releases correctly.
- Anthropic API requests now include the `thinking` parameter so thinking blocks are actually returned.

## v0.2.0

Complete rewrite from web-based agent to a single-binary terminal UI (TUI) built with [Bubble Tea](https://github.com/charmbracelet/bubbletea). The old web server, SQLite session store, and browser frontend have been removed.

### Added

- **TUI** -- Bubble Tea app with markdown rendering, syntax highlighting, input editor with history, token progress bar, slash command palette, bracketed paste, and external editor support (`Ctrl+E`)
- **Dual LLM adapters** -- Anthropic-wire and OpenAI-compatible SSE streaming with shared retry logic
- **Tool-call guardrails** -- whitelist-based auto-approve, four-option approval prompt (allow once / allow always / deny / deny + steer), `/yolo` toggle
- **Subagents** -- spawn isolated child agent sessions from within the agent loop, with per-session TUI strips and focus cycling
- **JSONL sessions** -- file-based session persistence with `--resume` to continue the most recent session
- **Config** -- global (`~/.gohome/settings.json`) and project-level (`./.gohome/settings.json`) endpoint configuration
- **CLI flags** -- `--version`, `--endpoint`, `--model`, `--yolo`, `--resume`
- **CI** -- cross-platform test matrix, lint, cross-build (linux/amd64, darwin/arm64, darwin/amd64, windows/amd64), binary size guard

### Removed

- Web server, browser frontend, SQLite session store, MCP client, Goreleaser config, Dockerfile

## v0.1.1

- File edit tool
- Documentation updates

## v0.1.0

- Initial release with web-based agent UI
- Anthropic LLM client with thinking blocks
- File read, file write, and shell tools
- SQLite-backed session persistence
- MCP client support
