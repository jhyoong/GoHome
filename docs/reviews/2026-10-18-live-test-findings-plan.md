# Live-test findings plan - 2026-10-18

Covers FOLLOWUP_TASKS items 58-62, found in the live test of `fix/outdated-components` (`docs/reviews/2026-10-18-outdated-components-live-test.md`). All five also exist on main.
Branch: `fix/live-test-findings`.

## 1. Compaction summary request (item 58)
- `agent/compact.go` sends `stripToolBlocks(oldMessages)` with the summary instructions only in the system prompt. The request often ends with an assistant tool call or a tool result, so the model continues the conversation instead of summarizing it.
- End the request with a user message asking for the summary.
- If the old messages already end with a user message, add the request to that message instead, so roles still alternate.
- Test: the summary request ends with a user message, for old messages ending with an assistant, tool and user message.

## 2. E2E compaction test (item 59)
- Re-run `TestE2EAutoCompact` against the local OpenAI endpoint after the fix.
- Also check that the summary text is a summary: not empty, and not a tool call written as text.

## 3. Tabs in markdown and code blocks (item 60)
- Tab-indented code lines leave stale cells from the spinner until a full redraw. Only tool entries expand tabs today (`tui/chat.go`).
- Expand tabs by display column in the markdown and code renderer.
- Test: a snapshot with a tab-indented code block.

## 4. Shell `cwd` error (item 61)
- `tools/shell.go` sets `cmd.Dir` without checking it. A missing folder gives `fork/exec /bin/sh: no such file or directory`.
- Check the folder first and return `cwd does not exist: <path>`. Also return an error if the path is not a folder.
- Test: a shell call with a missing `cwd` returns the new error.

## 5. Spinner during approval (item 62)
- The spinner says "Generating..." while the approval menu is open (`tui/model_agent.go`).
- Show "Waiting for approval" while the menu is open, and go back to "Generating..." after it closes.
- Test: a snapshot or a model test of the spinner text with an open approval prompt.

## 6. Docs
- Remove items 58-62 from `docs/FOLLOWUP_TASKS.md`.
- CHANGELOG Unreleased entry.
- Clear the TODO section for this PR.

## 7. Verification
- Write each test first and see it fail.
- `go vet ./gohome/...`, `go test ./gohome/...`, `golangci-lint run ./gohome/...`.
- Snapshot updates with `-update`, then review the diff.
- E2E `TestE2EAutoCompact` against the local endpoint only.
- Live test of the binary with the gohome-full-test skill, including auto-compaction with tool calls in history.

## Out of scope
- Other FOLLOWUP_TASKS items, including the other compaction and caching items (45).
