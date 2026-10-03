# Sudo Password Prompt Redesign

Date: 2026-10-03
Status: Approved

## Problem

The sudo password prompt is hard to notice and breaks on common inputs.

Root causes found in the current code:

1. **Password characters collide with menu shortcuts.** In
   `tui/model_approval.go`, `1`-`4`, `v`, `V` and Up/Down are matched as menu
   keys before reaching the password field. Typing `1` approves immediately
   with a partial password; `4` enters steer mode; `v` toggles the summary.
2. **Scrolling can move the menu selection.** The 2-second mouse idle timer
   disables mouse capture. Many terminals then send wheel scrolls as Up/Down
   arrow keys, which silently move the `>` marker.
3. **Layout overflows the terminal.** Chat height is computed for a 3-line
   editor (`tui/model.go`), but the approval box is ~10+ lines. The view is
   taller than the window, so Bubble Tea trims the top and the screen jumps,
   especially when PgUp/PgDn re-render.
4. **Silent password reuse.** The cached password is pre-filled without any
   indication. A wrong cached password is reused on every later prompt.

## Section 1: Two-step flow and key handling

**Step 1 -- approval menu.** For sudo commands, this is the normal approval
menu with no password field. `1`-`4`, `v`, `e` and arrows work as usual.
"Allow always" remains available; it whitelists the command pattern, never the
password.

**Step 2 -- password dialog.** Opens when the user picks Allow once or Allow
always on a sudo command and no cached password is in use. A `sudoStage` flag
on `approvalPrompt` tracks this; the reply is held until step 2 completes.

In step 2:

- All keys (digits, letters, arrows, pasted text) go to the password field.
  No shortcut matching.
- **Enter** submits the password and sends the decision chosen in step 1.
- **Esc** returns to the step 1 menu (so the user can still deny or steer).
- **Ctrl+C** denies (unchanged behaviour).
- Empty password + Enter does nothing and shows "Password required".
- PgUp/PgDn and mouse wheel still scroll the chat but cannot change the
  selection, because the menu is not active.

**Scroll safety in step 1.** While any approval is active, the mouse idle
timer does not disable mouse capture. This prevents wheel scrolls from being
turned into Up/Down keys. Normal idle behaviour resumes after the approval is
resolved.

## Section 2: Visibility and layout

**Password dialog appearance.** Replaces the approval box in the bottom input
region.

- Double-line border in a bright colour (ANSI 9), distinct from the yellow
  rounded approval border.
- Bold first line: `SUDO PASSWORD REQUIRED`, prefixed with `[<session-id>]`
  when the request comes from a subagent.
- The full sudo command, wrapped and capped at 3 lines.
- `Password: ` field with `*` per character and a cursor.
- Status line, empty by default; shows "Password required" after an empty
  Enter.
- Hint line: `Enter: run | Esc: back | Ctrl+C: deny`.
- The status bar shows `sudo password needed` while the dialog is open.

**Layout fix.** Render the input region first, count its lines, and compute
chat height as the window height minus the real input region height (plus the
other fixed sections). This fixes overflow for all approval boxes, not only
sudo.

**Cached password notice.** When caching is enabled and a cached password is
used, step 2 is skipped and a timeline notice is added:
`Using cached sudo password`.

## Section 3: Config, wizard and cache

**Setting.** New field on `config.Settings`:

```go
CacheSudoPassword bool `json:"cacheSudoPassword,omitempty"`
```

Defaults to `false`. Merged global -> project like other booleans. `main.go`
passes it into the TUI model.

**Setting off (default).** No password is stored. Step 2 opens for every sudo
command. `sudoPasswordCache` is only written when the setting is on.

**Setting on.**

- First sudo command opens step 2. On Enter, the password is stored in memory
  for this process only. Never written to disk or session JSONL.
- Later sudo commands skip step 2 and show the cached-password notice.
- **Wrong password:** when a shell tool result contains `Sorry, try again` or
  `incorrect password attempt`, the TUI clears the cache and adds the notice
  `Cached sudo password was rejected and cleared`. This matches sudo's
  standard messages only; systems with custom wording will not auto-clear.

**Wizard.** New step after "Config name", before "Confirm":

- Prompt: "Cache sudo password in memory for the session?"
- Options: `No` (default, first) -- "ask for the password on every sudo
  command"; `Yes` -- "ask once, reuse until gohome exits".
- Summary screen shows `Sudo cache:   on/off`; `save()` writes the field.
- Step counter text updated to the new total.

**Docs.** Add `cacheSudoPassword` to the README settings schema.

## Section 4: Testing

All tests are synchronous through `Model.Update`, following existing patterns.
No real sudo invocation.

**Key handling (`approval_test.go`)**

- Step 1 sudo menu: `1` moves to step 2 instead of replying; reply channel
  stays empty.
- Step 2: typing `1v4e` plus arrows puts only the runes in the field; Enter
  sends `SudoPassword == "1v4e"` with the outcome from step 1.
- Step 2: pasted multi-rune KeyMsg lands in the field.
- Step 2: Esc returns to step 1 without replying; Ctrl+C denies.
- Step 2: empty Enter sends nothing and shows "Password required".
- Allow always through step 2 still sends `SavedPattern`.

**Scroll safety**

- With an approval active, `mouseIdleMsg` does not disable mouse capture.
- PgUp/PgDn in step 2 scrolls the chat and leaves the field value unchanged.

**Layout**

- At a fixed size (e.g. 80x24), `View()` with an approval box and with the
  password dialog renders at most `winH` lines.

**Cache**

- Setting off: two sudo approvals in a row both open step 2.
- Setting on: second approval skips step 2, reply carries the cached
  password, notice is added.
- Setting on: shell tool result containing `Sorry, try again` clears the cache
  and adds the rejected notice.

**Config and wizard**

- `cacheSudoPassword` defaults to false; project overrides global.
- Wizard walks through the new step; saved JSON contains the value; summary
  shows the line.

**Snapshots**

- Update `sudo_approval_prompt.golden` (step 1, no password line).
- Add `sudo_password_dialog.golden` and
  `sudo_password_dialog_subagent.golden`.

**Manual check.** Build, run, trigger `sudo -k; sudo true`, and test a
password containing digits, wheel scrolling and PgUp/PgDn during both steps.
