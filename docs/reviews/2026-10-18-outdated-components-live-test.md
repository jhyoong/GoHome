# Live test of `fix/outdated-components` - 2026-10-18

Live test of the built binary against local endpoints. The goal was to check the tool overall, and how the branch changes affect the final binary.
Branch: `fix/outdated-components` (commit `92db916`).
Endpoints: `local-openai` (OpenAI wire, 192.168.50.210:4000) and `local-direct-anthropic` (Anthropic wire, 192.168.50.128:8080).

## 1. Binary changes from main

| | main | branch |
|---|---|---|
| Go toolchain | 1.26.4 (local default) | 1.26.9 (downloaded by `GOTOOLCHAIN=auto`) |
| chroma | v2.26.1 | v2.27.0 |
| goldmark | v1.8.2 | v1.8.6 |
| regexp2 | v2.1.1 | v2.2.1 |
| x/sys | v0.38.0 | v0.49.0 |
| Size, darwin/arm64, not stripped | 16.50 MB | 16.76 MB |

- `x/exp/golden` and `teatest` are test-only and are not in the binary.
- The block removed from `agent/compact.go` could not run. `History[prefixCount]` is a tool message only when the message before it is an assistant `tool_use`, and in that case `prefixCount` is already 0.

## 2. Build checks

- Release flags, all targets: linux/amd64 12.5 MB, darwin/arm64 12.0 MB, darwin/amd64 12.7 MB (stripped), windows/amd64 17.0 MB (not stripped). All under 25 MB.
- `govulncheck ./gohome/...`: no vulnerabilities.
- `go vet` and `go test ./gohome/...`: pass.
- `golangci-lint`: 0 issues with local v2.12.2. Not run with v2.15.0, the version CI pins.
- E2E suite (`-tags e2e`, `local-openai`): 11/11 pass. These tests call the packages, not the binary.

## 3. Binary tests

| Test | local-openai | local-direct-anthropic |
|---|---|---|
| Headless: write, edit, shell, read in one run (file checked on disk) | Pass | Pass |
| Headless: subagent | Pass | Pass |
| Headless JSONL (`--prompt - --verbose`): codeword kept across turns | Pass | Pass |
| Auto-compaction with tool calls in history | No API errors (5 compactions) | No API errors (8 compactions) |
| TUI: markdown and chroma highlighting | Pass | - |
| TUI: approval prompt, allow once | Pass | - |
| TUI: `/model` switch mid-session, deny a command | - | Pass |
| TUI: `--resume`, recall of earlier turns | - | Pass |
| TUI: `/quit` exits with no leftover process | Pass | Pass |

Compaction test setup: a scratch project with `contextWindow` 12000, `autoCompact` true and `autoCompactPct` 0.25. Four turns, each with about 1k tokens of filler and write and shell tool calls.

## 4. Finding: compaction summaries are not summaries

Compaction runs without errors, but the summary the model returns is usually not a summary. Examples from session logs:

- `[Tool call: shell({"command": "ls notes*.txt"})]`
- `<tool_call><function=shell>...ls notes*.txt...</function></tool_call>`
- `Tool use isn't available, so I can't create notes4.txt or run ls notes*.txt.`

The model continues the conversation instead of summarizing it. After this, the model lost track of its work: in two runs it created files nobody asked for (`notes5.txt`, `notes6.txt`).

The `main` binary does the same (3 of 3 summaries were a tool call written as text), so this was not caused by the branch.

Likely cause: `compact()` sends `stripToolBlocks(oldMessages)` with the summary instructions only in the system prompt. The request never ends with a user message asking for the summary, so the last message is often an assistant tool call or a tool result, and the model continues from there.

Possible fix: append a final user message such as "Summarize the conversation above." to the summary request, and add a test for it. Check that the request still alternates roles correctly when the old messages end with a user message.

Tracked as FOLLOWUP_TASKS item 58.

## 5. Second run (gohome-full-test skill)

A second run by a fresh agent using the `gohome-full-test` skill gave the same results for the binary changes, build checks, headless tests and TUI. golangci-lint v2.15.0 (the CI pin) also reports 0 issues. New findings, all also on main:

- `TestE2EAutoCompact` failed 2/2 on the branch and 2/2 on main with an empty summary. Same cause as section 4 (item 59).
- Tab-indented code lines in the TUI leave stale text from the spinner until the screen is redrawn (item 60).
- The shell tool gives a misleading `fork/exec /bin/sh` error when `cwd` does not exist (item 61).
- The spinner says "Generating..." while the approval menu is open (item 62).
