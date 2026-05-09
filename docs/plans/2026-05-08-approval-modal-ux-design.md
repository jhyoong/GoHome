# Approval Modal UX Improvements

## Summary

Three improvements to the tool approval modal in the frontend:

1. Reposition the card to appear just above the text input bar (no backdrop)
2. Make the card adaptive in size so the full command is visible without scrolling unless content is extremely long
3. Add arrow key navigation between the approval buttons

---

## 1. Positioning

**Current state:** The `.approval-modal` is a full-screen overlay (`position: absolute; inset: 0`) with a semi-transparent backdrop and the card centered in the viewport.

**New behavior:** No backdrop. The modal becomes a transparent anchor layer positioned just above the input bar:

```css
.approval-modal {
  position: absolute;
  bottom: 57px;   /* input bar height */
  left: 16px;
  right: 16px;
  z-index: 10;
  /* remove: inset, background, display:flex, align-items, justify-content */
}
```

The `.approval-card` drops its `max-width: 500px; width: 90%` constraints and fills the full anchored width. The input bar is 57px tall (12px top padding + 12px bottom padding + 1px border + ~32px content height).

---

## 2. Adaptive sizing

**Current state:** `.params` has `max-height: 200px; overflow: auto`, which forces a scroll even for moderately long commands.

**New behavior:**

- Remove `max-height` and `overflow` from `.params`
- Add to `.approval-card`:

```css
.approval-card {
  max-height: calc(100vh - 57px - 24px);
  overflow-y: auto;
}
```

The card grows to its natural height. Scroll only appears on the card itself when content would exceed the available viewport space above the input bar.

---

## 3. Keyboard shortcuts

When the modal is visible, a `keydown` listener on `document` handles:

| Key | Action |
|-----|--------|
| ArrowRight / ArrowDown | Focus next button (Allow → Deny → Always Allow, wraps) |
| ArrowLeft / ArrowUp | Focus previous button (wraps from Allow to Always Allow) |
| Enter | Click the focused button |
| Escape | Trigger Deny |

- On modal open: auto-focus the Allow button
- Button order: Allow → Deny → Always Allow
- When the always-allow editor sub-panel is open, arrow key interception is suspended so the pattern input field works normally
- The listener is added when the modal opens and removed when it closes

---

## Files to change

- `web/static/index.html` — no changes needed
- `web/static/app.css` — reposition modal, remove params max-height, add card max-height
- `web/static/app.js` — auto-focus on show, add/remove keydown listener, arrow key cycling logic
