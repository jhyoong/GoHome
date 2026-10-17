# Plan: Hangs and Crashes (H1-H5, plus A1)

Source: `docs/reviews/2026-10-17-codebase-audit.md`, section 2. A1 is from section 4.

Each step: write a failing test first, then fix, then run the package tests. None of these bugs is caught by the current tests.

## H1. Shell hangs when one output line is over 1 MiB

**Problem:** The scanner in `tools/shell.go:166-175` stops reading on `ErrTooLong`. The command blocks on a full pipe, so `cmd.Wait()` never returns. Timeout and Ctrl+C do not help.

**Fix:** After the scanner stops with an error, keep reading the pipe and discard the rest (`io.Copy(io.Discard, pr)`). Add a note to the result that the output was cut off.

**Test:** A command that prints a 2 MB single line with a short timeout must return quickly.

## H2. Shell timeout does not kill child processes

**Problem:** `exec.CommandContext` kills only `/bin/sh`. Children and background jobs keep the pipe open, so `Wait` blocks until they exit.

**Fix:**
1. Unix: set `cmd.Cancel` to kill the process group (`kill(-pid, SIGKILL)`). `Setsid` is already set in `shell_unix.go:14`.
2. All platforms: set `cmd.WaitDelay` (a few seconds).
3. On timeout or cancel, return the output captured so far with the timeout message (`shell.go:197-202`).

Windows: `WaitDelay` only. Killing the full process tree is out of scope.

**Tests:**
- `sleep 4; echo done` with a 300 ms timeout returns near the timeout.
- `sleep 30 & echo bg` returns near the timeout.
- Partial output is kept on timeout.

## H3. `WrapText` loops forever at small widths

**Problem:** In `tui/ansi.go:211-252`:
- A width of 0 or less never makes progress. Callers in `chat.go` subtract fixed offsets with no clamp.
- A 2-column character with 1 column left makes `takeWidth` return `""`, so the loop never progresses. This happens even at a positive width.

**Fix:** Inside `WrapText`:
1. Treat any width below 1 as 1.
2. If `takeWidth` returns an empty chunk on an empty line, take one grapheme anyway.

**Tests:**
- `WrapText` with widths 0, -5 and 1 on ASCII and CJK text finishes.
- `View()` at width 3 and with about 40 nested `>` quotes at width 80 finishes.

## H4. Horizontal rule panics on negative width

**Problem:** `strings.Repeat("─", width)` in `tui/markdown.go:126` panics when the width is negative.

**Fix:** Use a width of at least 1. Add the same guard to `tui/editor.go:340` (`width-len(indicator)`), which looks like it can also go negative. Not confirmed yet.

**Tests:** Rendering `---` at width 1, and `> > > > ---` at small widths, does not panic. Rendering the editor at a very small width does not panic.

## H5 + A1. Nil client crash and hidden errors

**Problem:**
- H5: if the setup wizard is cancelled or its save step fails (`main.go:754-772`, five silent returns), `client` stays nil. The first message calls `Stream` on nil (`agent/turn.go:37`) and crashes.
- A1: `Turn` errors are only logged (`main.go:152-153`). The TUI shows nothing; the spinner just stops.

**Fix:**
1. In `Turn`, if the client is nil, return an error such as "no model configured, run /config".
2. Emit `EventError` for errors from `Turn` / `Stream`, so the TUI shows them (A1).
3. In the wizard save step, show an error notice on each failure path instead of returning silently.

**Tests:**
- `Turn` with a nil client returns an error and does not panic.
- A `Stream` error produces an `EventError` that the TUI renders.

## Final check

- `go vet ./gohome/...`
- `golangci-lint run ./gohome/...`
- `go test ./gohome/...`
