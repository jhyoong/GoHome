# Live test: fix/llm-wire-streams-caching (HEAD fe01e17)

Binary tested: branch build vs a `main` build. Endpoints are local (192.168.x.x). Models: `local-openai`, `local-direct-anthropic`. Static checks (vet, tests, lint) were run earlier by the controller and were clean.

## Build
| Check | Result |
|---|---|
| Branch and base build | Pass |
| Cross-builds (linux, darwin x2, windows), size | Pass (12.3 to 17.5 MB, limit 25 MB) |
| Deps / toolchain diff vs main | None |
| govulncheck | Pass (branch and base clean) |

## E2E (`go test -tags e2e`)
| Model | Result |
|---|---|
| Configured E2E model (openai wire, Studio-General) | Pass |

## Headless (headless.sh)
| Model | T1 tools | T2 subagent | T3 multi-turn |
|---|---|---|---|
| local-openai | Pass | Pass | Pass on re-run (see note) |
| local-direct-anthropic | Pass | Pass | Pass |

Note: T3 failed once on local-openai (reply text did not contain "OK" for the first turn). Re-run 3 of 3 on the branch binary and 3 of 3 on the main binary gave "OK" then "maple-42". Treated as model flakiness, not a branch bug.

## Compaction (compact.sh)
| Model | Result |
|---|---|
| local-openai | Pass (4 compactions, summaries describe goal, files, progress; notes1..4 on disk) |
| local-direct-anthropic | Pass (4 compactions, summaries describe earlier turns; no tool errors) |

## Branch-specific checks

### A. promptCache (local-direct-anthropic, project settings, mode 600)
- Endpoint accepts the request: no 400. Two turns in one session ran fine (about 5k-token prompt).
- Cache usage is NOT reported by gohome. In the session JSONL and in the `turn_done` events, `inputTokens`, `cacheReadTokens` and `cacheWriteTokens` were all 0 on both turns.
- Cause: a direct request to the same endpoint (same body shape, `cache_control` on the system block) shows the endpoint sends `usage` with `input_tokens: 0` in `message_start`, and puts the real counts only in `message_delta` (`cache_creation_input_tokens: 6055` on call 1, `cache_read_input_tokens: 6050` on call 2). So caching works on the server. gohome reads input and cache tokens only from `message_start` (`translate.go`, `message_start` case) and only `output_tokens` from `message_delta`.
- Side effect: with `promptCache` on against this endpoint, `inputTokens` is 0, so the context meter and the compaction trigger (`agent/compact.go` sums usage) under-count. Without `promptCache` the same endpoint reports `inputTokens` 7115 and 7155 in `message_start`, on branch and on main.
- Main has no `promptCache`, so main does not hit this. Logged as follow-up 64.

### B. maxTokensField (local-openai, `max_completion_tokens`, `maxTokens` 200)
Pass. The endpoint accepted the request. The reply stopped at `outputTokens` 200 with `stopReason: length`, the notice "Response cut off: the model reached its output token limit (length)." was emitted, and the run ended normally with no error.

### D. Narrow TUI (tmux, 50 columns)
| Check | Result |
|---|---|
| `/resume` list within screen | Pass (no line over 50 columns). Long titles clip the age column ("just now" shows as "just", "1m ago" as "1m ag"). Main shows the same. Logged as follow-up 65 (minor). |
| `/model` list within screen | Pass (all rows 50 columns, description right-aligned) |
| Setup wizard option descriptions wrap | Pass. Run with `HOME` set to an empty scratch dir, so the real `~/.gohome` was not touched. The "How is your API key provided?" step wraps "read from an env var at / runtime" onto a second indented line. Wire step fits on one line. Only wizard steps 1 to 3 were walked; later steps were not reached. |

## TUI (120 columns, tmux)
| Check | local-openai | local-direct-anthropic |
|---|---|---|
| Markdown and code block render (5 distinct 256-colors) | Pass | not run |
| Approval: `1` runs the command, `3` denies, `Escape` denies; files checked on disk | Pass | not run |
| `/model local-direct-anthropic` switches status bar, settings.json hash unchanged | Pass (from local-openai) | n/a |
| `/quit` ends tmux session and process | Pass | not run |
| `--resume` reopens the last session, no picker, answers about an earlier turn | Pass | not run |

## Bugs
- 64: cache and input tokens are not read from `message_delta` usage (branch only, affects `promptCache` on this endpoint; main has no promptCache).
- 65: `/resume` list clips the age column at narrow widths (minor; also on main).

## Not tested
- The Anthropic fresh-redraw comparison for markdown, and the `[2] Allow always` approval (writes the whitelist).
- `--resume` plus `promptCache` across processes (two turns in one process only).
- Wizard steps after step 3.
- Against the real Anthropic API (not allowed: local endpoints only).

## Scratch dirs
`/tmp/gh-bin.DUnN` (binaries; the base worktree was removed), `/tmp/gh-proj.FdE5` (promptCache), `/tmp/gh-proj.btHF` (maxTokensField), `/tmp/gh-tui.u18w`, `/tmp/gh-tui.OBI3`, plus the dirs printed by the headless and compaction scripts (`/tmp/gh-work.*`, `/tmp/gh-proj.*`). Sessions are in `~/.gohome/sessions/<scratch-dir-name>-*`.
