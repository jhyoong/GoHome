# Tools Fix Plan - 2026-10-17

Plan for section 6 (T1-T3) of `2026-10-17-codebase-audit.md`, checked against branch `fix/tools-review`.

H1 (1 MiB line deadlock) and H2 (timeout does not kill children) are already fixed in `tools/shell.go`.

## Current state

| Item | Status | Notes |
|---|---|---|
| T1 shell output not limited | Open | `shell.go:176-187` keeps all output. A line over 1 MiB no longer hangs, but all output after it is dropped with a "cut off" note. |
| T1 read: no byte limit, fails on lines over 1 MiB | Open | `read.go:64` uses `bufio.Scanner` with a 1 MiB line limit. |
| T2 empty `old_string` | Open | `edit.go:61` |
| T2 CRLF multi-line edits fail | Open | The scanner strips `\r`, so the model never sees it. |
| T2 edit resets permissions to 0644 | Not a bug | Edit only writes files that already exist, and `os.WriteFile` keeps the existing mode. Checked with a small test program: a 0755 file stays 0755. |
| T3 `timeout_ms <= 0` times out at once | Open | `shell.go:119-124` |
| T3 description hard-codes 120000/600000 | Open | In both the description and the schema text. |
| T3 shell `cwd` relative to process dir | Not a live bug | The session folder is always `os.Getwd()` (`main.go:251`), nothing calls `Chdir`, and subagents use the parent's folder. |
| T3 read-before-edit compares raw paths | Open | `session.go:58-70`. Reading `foo.go` and then editing `./foo.go` fails. |

## Changes

### 1. Shell output limit (`tools/shell.go`)
- Replace the line scanner with a capped buffer that keeps the first 32 KiB and the last 32 KiB, with a `[... N bytes omitted ...]` marker between them.
- This also removes the 1 MiB line limit. Progress output only goes to `NullSink` today, so streaming chunks instead of lines changes nothing.

### 2. Read limits (`tools/read.go`)
- Use `bufio.Reader` instead of `bufio.Scanner`, so lines of any length work.
- Cut lines over 2000 characters, with a marker.
- Stop at 100 KiB of output, with a note like `truncated at line N; use offset=N+1`.

### 3. Edit fixes (`tools/edit.go`)
- Reject an empty `old_string` and point the model to `write`.
- CRLF: if there is no match and the file contains `\r\n`, try again with `\n` changed to `\r\n` in both `old_string` and `new_string`. Line endings stay as they were.
- No permissions change (not a bug).

### 4. Shell settings (`tools/shell.go`)
- Treat `timeout_ms <= 0` as the default timeout.
- Build the description and schema text from the configured default and maximum timeouts.

### 5. Path matching (`session/session.go`)
- `MarkRead` and `HasRead` turn paths into clean absolute paths (`filepath.Abs`) before storing or checking them.

### 6. Tests and docs
- Write a failing test first for each fix: long shell output, a long-line read, the read byte limit, empty `old_string`, a CRLF multi-line edit, timeout 0, the description text, and `./x` vs `x`.
- Add status lines to section 6 of the audit, like the other sections. Mark the permissions item as not a bug.
- Add the shell `cwd` item to `docs/FOLLOWUP_TASKS.md`.

## Out of scope
- Shell `cwd` resolution against the session folder (not a live bug).
- Changing `write` so it counts as a read. Today, after the model writes a file, it must read it before it can edit it.
