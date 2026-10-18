# Docs, CI and dead code plan - 2026-10-18

Covers the open items from audit sections 8 and 9, and the remaining unused code.
Branch: `fix/ci-and-outdated-docs`.

## State before this branch
- Section 8 (toolchain, CI, release) is fixed. CI passes on main. `release.yml` has not run since the rewrite (no tag pushed).
- The audit's dead code list is fixed. `deadcode` finds one more: `resetHighlightCache` (`tui/markdown.go`) is used only by tests.
- Section 9: Esc Esc, Ctrl+G, Ctrl+Y, Ctrl+D delete, the CHANGELOG Unreleased section and sudo in yolo were already fixed by earlier branches.

## 1. README
- `Shift+Enter` -> `Alt+Enter`.
- `/new` and `/resume`: remove "not configured until wired".
- `/config`: it is a select list (global, project, wizard), not "press `e`".
- Compaction prefix is `[Auto-compact summary]`, not `COMPACT:`.
- `contextWindow` default is 128000 and drives compaction.
- Deny ends the run. The model sees the denial on the next user message.
- `--prompt -` also requires `--yolo`.
- Add `/help` to the slash command table.
- Document the whitelist file format.

## 2. In-app help (help.go)
- `c` -> `Ctrl+Y`.
- Remove `/endpoint` and `--endpoint`.
- Add Ctrl+D, Esc Esc, `/clear-queue`, `/mouse`, `/config`, approval `1-4`/`v`/`e`, `--config`, `--prompt`, `--verbose`.

## 3. AGENTS.md
- Tools are shell, read, write, edit and subagent. `tools/session.go` is a context helper.
- Frontend also requires `SteerCh()`.

## 4. gohome/FUTURE.md
- Replace the "out of v0.2 scope" framing.
- Mouse: wheel scrolling shipped, click-to-focus not.
- Queued messages sub-point is fixed. The Windows paste item stays open (FOLLOWUP 54).

## 5. Dead code
- Move `resetHighlightCache` into `markdown_test.go`.

## 6. Audit doc and follow-ups
- Status lines in audit sections 8 (dead code) and 9.
- FOLLOWUP_TASKS: `release.yml` needs a test tag to verify.

## 7. Verification
- `go vet`, `go test ./gohome/...`, `golangci-lint run ./gohome/...`.
