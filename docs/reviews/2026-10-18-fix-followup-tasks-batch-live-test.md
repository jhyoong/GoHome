# Live test: fix/followup-tasks-batch (2026-10-18)

Branch `fix/followup-tasks-batch` (PR #62) merges four branches that fix FOLLOWUP_TASKS items 16-18, 21-32, 40, 42, 44, 64-66 and part of 37. Tested from commit `HEAD` of the branch, compared with `main` where a result needed it.

## Changes that reach the binary

- `session/load.go`, `list.go`, `validate.go`, `writer.go`: raw-byte decoding, early stop in `isBlank`, repaired results in `tool_use` order, line and newline written together, Warn on late emit (21, 22, 24, 27, 29).
- `cmd/gohome/main.go`: `closeWriter` reports write and close errors at exit in TUI and headless mode (26, 28).
- `agent/spawn.go`, `tools/tooluseid.go`, `session/events.go`, `tui/model_slash.go`: child IDs on disk are skipped, children record `parentToolUseId`, resume links by that ID and by raw task (30, 31, 32).
- `tui/config_wizard.go`: step 10/10 denylist confirmation, wrapped prompt and error line (16, 17).
- `guard/check.go`: debug log lines for guard decisions and prompts (18).
- `tui/approval.go`, `model_approval.go`: shared `scrollWindow()`, sudo dialog cap and scroll (42).
- `session/events.go`, `agent/state.go`: `model_change` record (44).
- `tui/editor.go`, `chat.go`, `pending.go`: sanitize at render time (65).
- `agent/compact.go`, `agent/run.go`: reject cut-off, empty or not-shorter summaries, wait after a rejection, `[Run failed]` marker (40, 64, 66).

No dependency, toolchain or workflow changes.

## Results

### Build and static checks

| Check | Result |
|---|---|
| `go vet` | Pass |
| `go test -count=1 ./gohome/...` | Pass |
| golangci-lint v2.15.0 | Pass (0 issues) |
| Cross-build, 25 MB guard | Pass (largest: windows/amd64 17420 KB) |
| govulncheck (branch and main) | No vulnerabilities |

### E2E (`local` endpoint 192.168.50.210, openai wire)

| Test | Result |
|---|---|
| 9 of 11 tests | Pass |
| `TestE2EWriteReadChain`, `TestE2EEditTool` | Failed once (model replied with only whitespace), then passed 3/3 re-runs on the branch and 3/3 on `main` |

The two failures are empty model replies, not branch behavior.

### Headless

| Test | local-openai | local-direct-anthropic |
|---|---|---|
| T1 tool chain | Pass | Pass |
| T2 subagent | Pass | Pass |
| T3 JSONL multi-turn | Pass | Pass |

In T2 the child's `session_start` holds `parentToolUseId`, and it equals the parent's subagent `toolUseId` on both models (31).

### Compaction

| Check | local-openai | local-direct-anthropic |
|---|---|---|
| Summaries describe earlier turns | Pass (6/6) | Pass (5/5) |
| Not-shorter summary rejected (66) | Not triggered | Pass: "Compaction skipped: the summary was not shorter than the messages it replaces." |
| Cut-off summary rejected, `maxTokens` 200 (64) | Pass: notice with `length`, twice in 6 turns | Pass: notice with `max_tokens`, twice in 6 turns |

No compaction grew the history (largest ratio 431 to 310).

### Failed run (40)

Model clone with `baseURL` `http://127.0.0.1:1` (connection refused):

| Check | local-openai | local-direct-anthropic |
|---|---|---|
| `[Run failed] <error>` assistant message saved, `stopReason` `error` | Pass | Pass |
| Resume on the real endpoint shows the marker as assistant text | Pass | Pass |
| Next turn is accepted by the API and the model sees the earlier turn was not answered | Pass | Pass |

### TUI (local-openai, 120x45 unless noted)

| Check | Result |
|---|---|
| Markdown and code colors | Pass (12 distinct 256-color codes) |
| Approval: `1` writes file, `3` and Escape do not | Pass |
| Guard debug log with `GOHOME_DEBUG=1` (18) | Pass: `guard prompt` and `guard decision` lines with tool, command, allow and reason (`user_once`, `user_denied`) |
| File search pick of `evil<ESC>[31mred.txt` and `bidi<U+202E>txt.exe` (65) | Pass: editor shows `@./evilred.txt  @./biditxt.exe`, no red SGR or U+202E on screen, also in the submitted user entry |
| `/model local-direct-anthropic` | Pass: status bar changes, `settings.json` hash unchanged, `model_change` record written (44) |
| `/quit` | Pass: process gone, stderr has no "session not saved" |
| `--resume --model local-openai` | Pass: same JSONL, earlier turns recalled, second `model_change` record written on resume (44) |
| Sudo dialog, 80x22, 120-word command (42) | Pass: Tab shows "(lines 1-10 of 14, PgUp/PgDn to scroll)", PgDn moves to lines 5-14, Ctrl+C denies. No password entered |
| Wizard, 50 columns, scratch `HOME` (16) | Pass: step 10/10 "Wrote <path>/denylist.json", Close. `settings.json` and `denylist.json` are `0600` |
| Wizard save error, `.gohome` made read-only before Save (17) | Pass: "Save failed: open <path>: permission denied" wraps over three lines |
| Two subagents with the same task, then resume (31) | Pass: `sub-1` and `sub-2` record the matching parent tool_use IDs (417 and 483 results match); both child views load on resume |
| Spawn after resume | Pass: new child is `sub-3` |
| Orphan child file `sub-4` with no spawn marker, then spawn (30) | Pass: new child is `sub-5`, `sub-4` untouched |

## Bugs and notes

- Wizard option descriptions and the notice line are cut off at narrow widths (for example "read from an env var at r" at 50 columns). `main` does the same. Added as item 67.
- In the expanded sudo dialog the "Tab: full command" hint disappears and no hint says how to collapse it. Minor, not logged.

## Not tested

- Items 26-28 (write errors at exit): no simple way to make the open session file fail live. Covered by unit tests.
- Items 21, 22, 24, 25, 29 and 32: no visible behavior; covered by unit tests.
- Item 64 and 66 with the TUI (tested headless only).
- Windows and Linux binaries were cross-built only.

## Scratch dirs

- Binaries: `/tmp/gh-bin.kjEA`
- Headless: `/tmp/gh-work.qonu`, `/tmp/gh-work.CsKq`
- Compaction and edge cases: `/tmp/gh-proj.3CVI`, `/tmp/gh-proj.INxv`, `/tmp/gh-proj.ViS5`, `/tmp/gh-proj.gFh8`, `/tmp/gh-proj.w4EH`, `/tmp/gh-proj.oA1L`
- TUI: `/tmp/gh-tui.XXKi`, `/tmp/gh-tui.I3On`, `/tmp/gh-tui.zSQf`, `/tmp/gh-tui.W0Tr`, `/tmp/gh-tui.BLfi`, `/tmp/gh-tui.W7ue`
- Session logs: `~/.gohome/sessions/<scratch-dir-name>-*`
