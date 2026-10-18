# Live test: fix/approval-whitelist (FOLLOWUP_TASKS 5, 6, 7, 9, 10, 14)

Date: 2026-10-18. Binary built from the working tree of `fix/approval-whitelist`, compared with `main` (bb7e72d). Endpoints are local (192.168.50.210). Models: `local-openai`, `local-direct-anthropic`.

## What changed

- `guard/suggest.go`: no suggestion for commands with a shell operator, sudo included (5).
- `guard/compile.go`: a tab is a word boundary (9); `\r` is a shell operator (10).
- `guard/persist.go`: new project `whitelist.json` is `0600`, its folder `0700` (14).
- `tui/approval.go`, `tui/model_approval.go`: "(e edit)" shown for an empty pattern and hidden for sudo; sudo with no pattern shows "not available for this command"; Up/Down clear the error line (6, 7).
- Snapshot `sudo_approval_prompt.golden`: the sudo line no longer shows "(e edit)", since `e` never worked there.

## Results

### Build and static checks

| Check | Result |
|---|---|
| `go vet` | Pass |
| `go test -count=1 ./gohome/...` | Pass |
| golangci-lint v2.15.0 (CI pin) | Pass, 0 issues |
| Size, base vs branch | Same (16772402 bytes) |
| Deps and toolchain | No difference |
| Cross-build, 4 targets | Pass, all under 25 MB |
| govulncheck | No vulnerabilities, base and branch |

### E2E (local-openai wire)

| Test | Result |
|---|---|
| 10 of 11 tests | Pass |
| `TestE2EToolErrorRecovery` | Failed once in the full run, then passed 4 of 4 re-runs on the branch and 3 of 3 on `main`. The first failure message was not captured. The test does not reach the changed code. Treated as a flaky model reply. |

### Headless

| Test | local-openai | local-direct-anthropic |
|---|---|---|
| T1 write, edit, shell, read | Pass | Pass |
| T2 subagent | Pass | Pass |
| T3 JSONL multi-turn | Pass | Pass |

### Compaction

| Model | Result |
|---|---|
| local-openai | Pass (4 summaries, all describe earlier turns) |
| local-direct-anthropic | Pass (7 summaries, all describe earlier turns) |

On `local-direct-anthropic`, one compaction went from 114 to 125 tokens and another from 56 to 41. `main` shows the same pattern (94 to 88, 146 to 136; 9 compactions in one run). Added as FOLLOWUP_TASKS item 66.

### TUI (local-openai, tmux 120x45)

| Check | Result |
|---|---|
| Chained command `echo one > a.txt; echo two >> a.txt` shows no pattern and `[2] Allow always  (e edit)` (5, 7) | Pass |
| `2` on it shows `Error: pattern is empty`; Down clears the error (7) | Pass |
| `1` runs it; `a.txt` holds `one`, `two` | Pass |
| `ls -la` suggests `^ls  (e edit)`; `2` saves it; `.gohome` is `drwx------`, `whitelist.json` is `-rw-------` (14) | Pass |
| Global `~/.gohome/whitelist.json` hash unchanged | Pass |
| `ls<TAB>-la` (model sent `ls\t-la`) runs with outcome `whitelisted`, no prompt (9) | Pass |
| `sudo -n true; echo done` shows `[2] Allow always  (not available for this command)`; `2` shows the error and does not open the password stage; `3` denies (6) | Pass |
| Esc denies `touch esc.txt`; no file created | Pass |
| Markdown: heading, list, bold, Go code block with 5 distinct 256-color codes | Pass |
| `/model local-direct-anthropic` changes the status bar; settings hash unchanged | Pass |
| `/quit` ends the tmux session and the process | Pass |
| `--resume` reopens the same JSONL (30 to 32 lines) and recalls the first command | Pass |

## Not tested

- Item 10 (`\r` as an operator) live: a command with `\r` has no suggestion, so it needs a hand-written pattern. Covered by unit tests only.
- `TUI` with `local-direct-anthropic` beyond the model switch.
- Windows (file mode test is skipped there).

## Scratch dirs

- `/tmp/gh-bin.5LFw` (binaries; base worktree removed)
- `/tmp/gh-work.9eNb`, `/tmp/gh-work.42fx` (headless)
- `/tmp/gh-proj.1jzo`, `/tmp/gh-proj.swrI`, `/tmp/gh-proj.X5uu` (compaction, last one on base)
- `/tmp/gh-tui.b4z6` (TUI)
- Sessions: `~/.gohome/sessions/<scratch-dir-name>-*`
