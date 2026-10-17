# Follow-up Tasks

Open items found while fixing and manually testing the 2026-10-17 security fixes (PR #48). None of these are regressions from that PR.

## Denylist

1. **A project file can turn off the built-in denylist.** If there is no global `~/.gohome/denylist.json` (for example the user chose "No" in the setup wizard), a repo can ship `.gohome/denylist.json` with `{"shell":[]}`. The built-in rules are then dropped, and `rm -rf /` and `rm -rf ~` are not denied. Approval prompts still apply when yolo is off. Related to audit S2, which was marked as not an issue.
2. **Bypass forms that are not denied.** These still prompt when yolo is off:
   - `rm / -rf` (target before the flag)
   - `rm -rf //` and `rm -rf /.`
   - `bash -c "rm -rf /"` and `sh -c 'rm -rf ~'`
   - `find / -delete` and similar commands
3. **False positive.** `echo "; rm -rf /"` is denied, because the `;` inside the quotes is treated as a separator.
4. **Write and edit tools are not checked by the denylist.** Excluded from the security fixes plan.

## Whitelist and approval prompt

5. **"Allow always" on a chained command saves a rule that never matches.** For example `git status; echo x` suggests `^git status;`. Chained commands are never auto-approved, so the rule is useless. Offer no suggestion for commands with shell operators.
6. **Sudo commands with no suggestion cannot be saved.** Editing the pattern is disabled for sudo, so "Allow always" shows "pattern is empty". "Allow once" and "Deny" still work.
7. **No "(e edit)" hint when the pattern is empty,** and the `Error:` line stays visible while moving with the arrow keys.
8. **Old `^sudo` whitelist entries stay active.** Entries saved by the old suggestion still allow every sudo command (silently with the password cache on). Consider a warning in `Compile` or a migration.
9. **A tab is not a word boundary** in whitelist matching, so `ls<TAB>-la` prompts. Fails closed.
10. **`\r` is not treated as a shell operator** in `hasShellOperator`. Only matters for hand-written patterns, since commands with control characters get no suggestion.

## Terminal output

11. **Unicode bidi characters are not flagged.** Characters such as U+202E can visually reorder text in the approval prompt. Neither the sanitizer nor the control-character warning handles them.
12. **Unknown-tool summaries are cut to length before control characters are escaped,** so the cut point is computed on the raw text.
13. **Filenames in the file search list, session browser and `/resume` picker are not sanitized.**

## File permissions

14. **`whitelist.json` is created `0644`.**
15. **Existing `~/.gohome` folders created `0755` by older versions keep that mode.** Consider a one-time warning.

## Setup wizard

16. **No confirmation after the denylist step.** The wizard shows "step 9/10" and closes without saying that `denylist.json` was written.
17. **The wizard error line is not wrapped** to the terminal width.

## Logging

18. **`GOHOME_DEBUG=1` only logs "gohome started".** Guard decisions (whitelisted, denied, prompted) appear only in session files. Add debug log lines for guard decisions.

## Testing gaps

19. **The approval-prompt control-character warning was not tested manually.** The model removed raw ESC bytes from its tool calls. Covered by unit tests only.
20. **Bare `rm -rf /` forms were not run live.** The model refused them. They were tested as non-running forms (`false && ...`) and by calling the denylist check directly.
