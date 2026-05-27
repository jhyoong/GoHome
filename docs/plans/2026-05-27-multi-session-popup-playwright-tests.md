# Multi-Session Tool Popup — Playwright CLI Test Plan

> **For Claude:** Use the `playwright-cli` skill to execute the scenarios
> below. Each scenario is independent; restart from preconditions
> between runs. Report pass/fail per scenario with the matching DOM
> evidence.

## Preconditions

1. Fresh DB: delete or move aside the session DB file. Check
   `internal/session/store.go` for the default path; typically
   `~/.gohome/sessions.db` or similar.
2. Start the server: `go run ./cmd/agent` (or the project's run command —
   check `cmd/` for the actual entry).
3. Confirm it serves on `http://localhost:PORT` (note the actual port
   from server logs).
4. Confirm the agent config (`~/.gohome/config.yaml` or
   `config.example.yaml`) lets the agent invoke a tool that requires
   approval. The `shell` tool with a non-whitelisted command like `ls`
   is the most reliable trigger. Confirm `auto_approve_all` is `false`
   and there is no whitelist entry that would match `ls`.

## Selectors

- Approval modal: `#approval-modal` (visible iff its `hidden` attribute
  is absent).
- Allow button: `#approval-allow`.
- Deny button: `#approval-deny`.
- Always-Allow button: `#approval-always-allow`.
- Toast container: `#toast-stack`.
- Individual toast: `#toast-stack .toast`.
- Toast click target: the toast itself (the click listener is on the
  outer div).
- Sidebar session item by session ID: `.session-list li[data-id="<sessionID>"]`.
- Chat input textarea: `#input`.
- Send button: `#send-btn`.

## Tab identity

The frontend generates a random `tabID` per page load. Reloading a tab
creates a fresh WS connection. To capture `activeSessionId` in a test:
`await page.evaluate(() => activeSessionId)`.

## Scenario 1 — Fan-out to all tabs viewing one session

1. Open browser context A, navigate to the app, focus the chat input,
   type a message that asks the agent to run a shell command (e.g.
   "list the files in the current directory using shell").
2. Click `#send-btn`. Wait for the agent to call the `shell` tool — the
   `#approval-modal` becomes visible in Tab A.
3. Capture the active session ID:
   `const sid = await pageA.evaluate(() => activeSessionId);`
4. Open a second browser context B (separate cookies acceptable).
   Navigate to the same URL. Wait for the sidebar to render. Click the
   session item matching `sid` (selector
   `.session-list li[data-id="${sid}"]`).
5. **Assert:** Within 2 seconds, `#approval-modal` is visible in Tab B
   AND remains visible in Tab A.

**Pass:** Modal visible in both tabs.
**Fail:** Modal not in B; or modal disappears from A; or any console
errors.

## Scenario 2 — First responder wins

Continuing from Scenario 1 (both tabs show the modal):

1. In Tab B, click `#approval-allow`.
2. **Assert:** Within 1 second, `#approval-modal` has `hidden` attribute
   in BOTH tabs.
3. **Assert:** Tab A receives a `tool_result` rendering — look for a new
   `.tool-call-block` element with the `approved` status indicator
   (`.tool-call-status.approved`).
4. **Assert:** No console errors in either tab.

**Pass:** Both modals dismiss; agent continues with the approved
result.
**Fail:** Either modal lingers; or the agent does not continue; or
console errors.

## Scenario 3 — Late-join replay

1. From a clean state (kill server, wipe DB, restart server), repeat
   Scenario 1 step 1-2 — Tab A shows the modal for session X. Capture
   session ID `sid`.
2. Close Tab A's browser context entirely.
3. Wait at least 2 seconds (allow the server to detect WS close and run
   `unwatchAll`).
4. Open a fresh browser context C. Navigate to the app. Click the
   sidebar item for `sid`.
5. **Assert:** Within 2 seconds of loading the session,
   `#approval-modal` is visible in Tab C.
6. Click `#approval-allow` in Tab C.
7. **Assert:** Modal dismisses; the agent continues (new assistant
   content streams in or a `done` event is received — look for an
   eventual `.message.message-assistant` with content or the input
   re-enabling).

**Pass:** Late tab sees the pending modal via replay.
**Fail:** No modal in Tab C; or the request times out (after 5 min).

## Scenario 4 — Cross-session toast

1. From a clean state, open Tab A, send a `shell` request — confirm
   modal in Tab A. Capture `sidA`.
2. Open Tab B in a separate context. Click "New Chat" or send an
   innocuous message to create a NEW session — make sure the modal in
   Tab A is still pending. Capture `sidB`. (Hint: send a message in
   Tab B that does NOT require approval, like "Hello".)
3. **Assert:** Within 2 seconds, `#toast-stack .toast` is present in
   Tab B. Verify its text content includes "Tool approval needed" and
   the tool name (e.g. `shell`).
4. **Assert:** Tab B's `#approval-modal` is NOT visible (still has
   `hidden`).
5. **Assert:** Tab B's `activeSessionId === sidB` (not `sidA`).

## Scenario 5 — Toast click navigates

Continuing from Scenario 4:

1. In Tab B, click the toast: `await pageB.click('#toast-stack .toast');`
2. **Assert:** `await pageB.evaluate(() => activeSessionId)` returns
   `sidA`.
3. **Assert:** Within 2 seconds, `#approval-modal` is visible in Tab B
   (this is the replay).
4. **Assert:** `#toast-stack .toast` count in Tab B is 0 (the toast was
   removed on click and again on `onHistory`).

## Scenario 6 — Resolution toast cleanup

From Scenario 4 state (Tab A has modal, Tab B has toast):

1. In Tab A, click `#approval-allow`.
2. **Assert:** Within 1 second, Tab B's toast is removed
   (`#toast-stack .toast` count is 0 in Tab B).
3. **Assert:** Tab A's modal is dismissed.
4. **Assert:** No console errors.

**Pass:** Toast in non-viewing tab clears when the originating tab
resolves.

## Scenario 7 — Concurrent click (best-effort)

1. From Scenario 1 state (both tabs show modal).
2. Schedule clicks in parallel:
   ```js
   await Promise.all([
     pageA.click('#approval-allow'),
     pageB.click('#approval-deny'),
   ]);
   ```
3. **Assert:** Both modals dismiss; no console errors in either tab.
4. **Assert:** Exactly one outcome reaches the agent — wait for the
   next `tool_result` rendering in either tab and check its
   `.tool-call-status` class: it will be either `approved` or `denied`,
   matching whichever tab's click won the CompareAndSwap race. The
   other tab's response is silently dropped server-side.

**Pass:** Single decisive outcome; no hangs; no console errors.
**Fail:** Both outcomes register; or the agent hangs; or modals
linger.

## Cleanup after each scenario

1. Kill the server (Ctrl-C in the terminal).
2. Delete the session DB file.
3. Close all browser contexts.
4. Restart fresh.

## Pass/Fail Rubric

Each scenario must pass independently. Report per scenario:

- **Pass:** all assertions met, no console errors. Cite the assertion
  results and any DOM-state evidence.
- **Fail:** any assertion missed. Capture a screenshot via
  `pageX.screenshot()` and quote the exact assertion that failed plus
  any console messages.
- **Skipped:** only if a precondition cannot be met (e.g. `shell` tool
  unavailable, server fails to start). Document why and what would be
  needed to run.

## Out-of-scope behaviors (do NOT test in this run)

- Server-restart persistence of pending approvals (broker is in-memory).
- Approval history UI.
- Multi-user / auth.
- Subagent prompt forwarding to non-originating tabs (separate bug,
  separate fix).
