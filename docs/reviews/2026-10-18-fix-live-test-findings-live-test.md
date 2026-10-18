# Live test: fix/live-test-findings - 2026-10-18

Branch fixes FOLLOWUP_TASKS items 58-62 (plan: `docs/reviews/2026-10-18-live-test-findings-plan.md`). Tested from the working tree, before commit. Endpoints are local only. Models: `local-openai` and `local-direct-anthropic`.

## Build

| Check | Result |
|---|---|
| `go vet`, `go test -count=1`, golangci-lint v2.15.0 | Pass, 0 issues |
| Size | base 16755426, branch 16772018 bytes |
| Deps and toolchain | No change |
| Cross-builds (linux, darwin arm64/amd64, windows) | Pass, all under 25 MB |
| govulncheck base and branch | No vulnerabilities |

## E2E

All 11 tests pass against the local OpenAI endpoint. `TestE2EAutoCompact` (item 59) passes and the summary is a summary: "The user sequentially requested detailed explanations about topic 1, topic 2, ... topic 8. ..."

## Headless

| Test | local-openai | local-direct-anthropic |
|---|---|---|
| T1 tool chain | Pass | Pass |
| T2 subagent | Pass | Pass |
| T3 JSONL multi-turn | Pass | Pass |

Both models hit `edit: file must be read first` after `write`. This is known item 52.

## Compaction (item 58)

| Model | Summaries | Result |
|---|---|---|
| local-openai | 4 of 4 describe the earlier turns, files and pending work | Pass |
| local-direct-anthropic | 4 of 4 describe the earlier turns, files and pending work | Pass |

No summary was empty, a tool call, a refusal, or a continuation. No tool errors.

## TUI (local-openai, tmux 120x45)

| Check | Result |
|---|---|
| Markdown and tab-indented Go code (item 60) | Pass. The session log has `\t\t` in the reply; the screen shows spaces, 6 highlight colors, no leftover spinner text. A fresh `--resume` redraw matches. |
| Spinner during approval (item 62) | Pass. Shows "Waiting for approval..." while the menu is open. |
| Allow once | Pass. `a.txt` written. |
| Deny, Escape | Pass. No `b.txt` or `c.txt`. |
| Missing `cwd` (item 61) | Pass. Model reported `shell: cwd does not exist: /tmp/does-not-exist-xyz`. |
| Model switch | Pass. Status bar shows `local-direct-anthropic`; settings.json hash unchanged. |
| Quit | Pass. tmux session gone, no gohome process. |
| Resume | Pass. Same JSONL (22 -> 24 lines); model recalled the `a.txt` content. |

## Bugs

None new.

## Not tested

- `[2] Allow always` (writes to the whitelist).
- TUI with `local-direct-anthropic` (only the model switch).
- "cwd is not a folder" error in the binary (unit test only).
- Windows and Linux binaries were built, not run.
