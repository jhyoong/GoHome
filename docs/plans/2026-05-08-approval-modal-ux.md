# Approval Modal UX Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Reposition the approval modal above the input bar, make it adaptive in size, and add arrow key navigation between buttons.

**Architecture:** Two files change — `app.css` for layout/sizing and `app.js` for focus management and keyboard handling. No HTML changes. The modal loses its full-screen overlay and becomes a positioned element anchored just above the input bar.

**Tech Stack:** Vanilla JS, plain CSS, no build step — edit files directly in `web/static/`.

---

### Task 1: Reposition the modal and remove the backdrop (CSS)

**Files:**
- Modify: `web/static/app.css`

The `.approval-modal` currently uses `inset: 0` (full screen), a semi-transparent backdrop, and flex centering. Replace all of that with a position anchored above the input bar. The `.approval-card` currently has `max-width: 500px; width: 90%` — remove those so the card fills the anchored width.

**Step 1: Update `.approval-modal`**

Replace the existing `.approval-modal` rule:

```css
/* BEFORE */
.approval-modal {
  position: absolute;
  inset: 0;
  background: rgba(0,0,0,0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 10;
}
```

With:

```css
/* AFTER */
.approval-modal {
  position: absolute;
  bottom: 57px;
  left: 16px;
  right: 16px;
  z-index: 10;
}
```

**Step 2: Update `.approval-card`**

Remove `max-width: 500px;` and `width: 90%;` from `.approval-card`. Add `max-height` and `overflow-y` so the card can scroll as a whole when content is extremely long:

```css
/* AFTER */
.approval-card {
  background: #fff;
  border-radius: 10px;
  padding: 24px;
  box-shadow: 0 8px 32px rgba(0,0,0,0.2);
  max-height: calc(100vh - 57px - 24px);
  overflow-y: auto;
}
```

**Step 3: Update `.params`**

Remove `max-height: 200px;` and `overflow: auto;` from `.params` so it no longer forces an early scroll:

```css
/* AFTER */
.params {
  background: #f5f5f5;
  padding: 12px;
  border-radius: 6px;
  margin-bottom: 16px;
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-word;
}
```

**Step 4: Verify visually**

Open the app in a browser, trigger a tool approval, and confirm:
- Card appears just above the input bar with no dark overlay
- Card fills the horizontal width of the chat area
- Long commands are fully visible without an inner scroll box

**Step 5: Commit**

```bash
git add web/static/app.css
git commit -m "feat: reposition approval modal above input bar, adaptive sizing"
```

---

### Task 2: Auto-focus Allow button when modal opens (JS)

**Files:**
- Modify: `web/static/app.js`

When the modal appears, focus should land on the Allow button immediately so the user can press Enter to approve or arrow to another option.

**Step 1: Add focus call in `showApprovalModal()`**

Find the end of `showApprovalModal()`. After `dom.approvalModal.hidden = false;`, add:

```js
dom.approvalAllow.focus();
```

The full end of the function should look like:

```js
  dom.approvalModal.hidden = false;
  dom.input.disabled = true;
  dom.sendBtn.disabled = true;
  dom.approvalAllow.focus();
}
```

**Step 2: Verify**

Trigger an approval modal. The Allow button should have visible focus styling immediately (browser default outline or your button focus style).

**Step 3: Commit**

```bash
git add web/static/app.js
git commit -m "feat: auto-focus Allow button when approval modal opens"
```

---

### Task 3: Arrow key navigation between approval buttons (JS)

**Files:**
- Modify: `web/static/app.js`

Add a `keydown` handler that cycles focus between the three approval buttons, activates the focused button on Enter, and denies on Escape. The handler is only active while the modal is open. When the always-allow editor sub-panel is visible, arrow key interception is suspended so the pattern text field works normally.

**Step 1: Add `handleApprovalKeys` function**

Add this function near the other modal-related functions (`showApprovalModal`, `hideApprovalModal`):

```js
function handleApprovalKeys(e) {
  // When the pattern editor is open, don't intercept arrow keys
  if (!dom.alwaysAllowEditor.hidden) return;

  const buttons = [dom.approvalAllow, dom.approvalDeny, dom.approvalAlwaysAllow]
    .filter(btn => !btn.hidden);

  const focused = document.activeElement;
  const idx = buttons.indexOf(focused);

  if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
    e.preventDefault();
    const next = buttons[(idx + 1) % buttons.length];
    next.focus();
  } else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
    e.preventDefault();
    const prev = buttons[(idx - 1 + buttons.length) % buttons.length];
    prev.focus();
  } else if (e.key === 'Escape') {
    e.preventDefault();
    dom.approvalDeny.click();
  }
  // Enter is handled natively by the focused button — no extra code needed
}
```

Note: `buttons` is filtered to exclude hidden buttons (Always Allow is hidden for chained shell commands), so the cycle stays correct in all cases.

**Step 2: Register and deregister the listener**

In `showApprovalModal()`, after `dom.approvalAllow.focus();`, add:

```js
document.addEventListener('keydown', handleApprovalKeys);
```

In `hideApprovalModal()`, at the top of the function, add:

```js
document.removeEventListener('keydown', handleApprovalKeys);
```

The full `hideApprovalModal` start should be:

```js
function hideApprovalModal() {
  document.removeEventListener('keydown', handleApprovalKeys);
  state.awaitingApproval = null;
  // ... rest unchanged
```

**Step 3: Verify**

Trigger an approval modal and confirm:
- Allow button is focused on open
- ArrowRight moves focus to Deny, then to Always Allow, then wraps back to Allow
- ArrowLeft moves focus in reverse
- Enter on a focused button activates it
- Escape triggers deny
- Clicking "Always Allow" opens the pattern editor; inside it, arrow keys move the text cursor normally (not cycle buttons)

**Step 4: Commit**

```bash
git add web/static/app.js
git commit -m "feat: arrow key navigation between approval modal buttons"
```
