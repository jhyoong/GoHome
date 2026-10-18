# Live test: fix/terminal-output (2026-10-18)

Branch `fix/terminal-output` fixes FOLLOWUP_TASKS items 11, 12 and 13. The changes were tested from the working tree (not yet committed).

## Changes that reach the binary

- `tui/sanitize.go`: new `isBidi` (U+061C, U+200E, U+200F, U+202A-U+202E, U+2066-U+2069). `sanitizeText` removes these characters. `showControls` writes them as `\uNNNN` and sets `found`, so the approval prompt shows the control-character warning (11).
- `guard/suggest.go`: `hasControlChar` counts bidi characters, so a command that contains one gets no suggested pattern (11).
- `tui/chat.go`, `tui/approval.go`: `rawToolArg` no longer calls `shortSummary`. It returns a `fallback` flag, and callers cut the text after `sanitizeText` (timeline) or `showControls` (approval prompt) (12).
- `tui/filesearch.go`: file names are passed through `sanitizeText` for display. The selected path is unchanged (13).
- `tui/session_browser.go`: session titles are passed through `sanitizeText` in `listingToItem`. The `/resume` picker uses this function through `NewSessionBrowser` (13).

No dependency, toolchain or workflow changes.

## Results

### Build and static checks

| Check | Result |
|---|---|
| `go vet` | Pass |
| `go test -count=1 ./gohome/...` | Pass |
| golangci-lint v2.15.0 | Pass (0 issues) |
| Cross-build, 25 MB guard | Pass (largest: windows/amd64 17396 KB) |
| govulncheck (branch and main) | No vulnerabilities |

### E2E (`local` endpoint 192.168.50.210, openai wire)

| Test | Result |
|---|---|
| All 11 tests | Pass |

### Headless

| Test | local-openai | local-direct-anthropic |
|---|---|---|
| T1 tool chain | Pass | Pass |
| T2 subagent | Pass | Pass |
| T3 JSONL multi-turn | Pass | Pass |

### Compaction

| Model | Summaries | Result |
|---|---|---|
| local-openai | 10 | Pass |
| local-direct-anthropic | 9 | Pass |

Every summary describes the earlier turns. No tool errors. Three local-direct-anthropic summaries name a working directory (`/home/taylor/projects/dashboard`) that does not exist. The files were written to the right scratch dir, so this is the model's wording, not a gohome bug.

### TUI (local-openai)

| Check | Result |
|---|---|
| Markdown and code (256-color tokens, tab expanded) | Pass |
| Approval `[1]` Allow once (file created) | Pass |
| Approval `[3]` Deny (no file) | Pass |
| Approval `Esc` (no file) | Pass |
| `/model local-direct-anthropic` (status bar changes, settings hash unchanged) | Pass |
| `/quit` (tmux session and process gone) | Pass |
| `--resume` (same JSONL, model recalls the first `touch` file) | Pass |

### Item-specific checks (branch vs main)

The scratch dir held two files, `evil<ESC>[31mRED<ESC>[0m.txt` and `evilt<ESC>]2;PWNED<BEL>.txt`. A headless session was created there first with a prompt that contains `<ESC>]2;TITLEPWN<BEL>` and `<ESC>[31m`, so it has a hostile title.

| Check | Branch | main |
|---|---|---|
| File search `@evil` (`capture-pane -e`, pane title) | Pass: `./evilRED.txt` and `./evilt.txt`, no color codes, pane title unchanged | Fail: `evil\x1b[31mRED` is drawn red, pane title becomes `PWNED` |
| `/resume` picker with the hostile title | Pass: `Reply with only OK. tag  REDTITLE end`, no color codes, pane title unchanged | Fail: `REDT` drawn red, pane title becomes `TITLEPWN` |
| Approval prompt for a command with U+202E | Pass: `shell: echo safe #\u202ehs.live` plus the control-character warning; timeline shows the command with U+202E removed | Fail: raw U+202E in the prompt, no warning |

The model kept U+202E in the command on both binaries (on main it chose a different command, `printf ... | od -An -c`, still with a raw U+202E). Each prompt was denied with `[3]`.

Not tested in the TUI (unit tests only): the suggested pattern for a bidi command (testing `[2]` writes the whitelist) and item 12 (no tool in the binary returns the unknown-tool fallback).

## Not tested

- Item 12 in the binary. It needs an unknown tool name or input that fails to parse; covered by `TestExtractToolArg_UnknownToolCutAfterSanitize` and `TestApproval_UnknownToolCutAfterShowControls`.
- Suggested whitelist pattern for a bidi command in the TUI (covered by `TestSuggest_BidiGivesNoSuggestion`).
- Windows.

## New issues

- **65** The editor and live user entries show raw control characters. After picking `evil<ESC>[31mRED<ESC>[0m.txt` from file search, the editor drew `RED` in red. A typed or pasted U+202E also stayed raw in the user timeline entry. Resumed sessions are sanitized. This code is unchanged from main.

Scratch dirs: `/tmp/gh-bin.VtCq`, `/tmp/gh-work.aFgS`, `/tmp/gh-work.8tPq`, `/tmp/gh-proj.45n7`, `/tmp/gh-proj.5Saf`, `/tmp/gh-tui.rHg7` (branch), `/tmp/gh-tui.YnK2` (main).
