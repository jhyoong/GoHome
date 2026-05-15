# Always-Visible Thinking Blocks Design

**Date:** 2026-05-15
**Branch:** feature/thinking-blocks

## Problem

The current thinking block toggle has two click handlers firing simultaneously: an inline `onclick` attribute on the `<button>` and a delegated event listener on the messages container. When a user expands one thinking block, both handlers fire and cause unpredictable DOM state — other thinking blocks disappear or become unresponsive.

## Decision

Remove the collapse/expand toggle entirely. Show thinking content always-visible with a plain "Thinking" label. Italic text distinguishes thinking from regular message content.

## Changes

### `web/static/app.js` — `thinkingBlockHtml`

Replace `<button data-thinking-toggle onclick="...">` with a plain `<div class="thinking-header">`. Remove `hidden` from `.thinking-body`. Remove the toggle icon span.

```html
<div class="thinking-block">
  <div class="thinking-header">
    <span class="thinking-label">Thinking</span>
  </div>
  <div class="thinking-body">${escHtml(thinking)}</div>
</div>
```

### `web/static/app.js` — delegated click listener

Remove the `thinkingHeader` branch (lines ~536–546). The `data-thinking-toggle` attribute no longer exists in the HTML.

### `web/static/app.css`

- `.thinking-header`: remove `cursor: pointer` and `:hover` background rule.
- `.thinking-body`: add `font-style: italic`.
- Remove `.thinking-toggle` rule.

## Out of Scope

No changes to `handleThinkingToken`, `addToolResult`, or `finalizeStream`. The streaming logic is correct; the bug was in the toggle mechanism only.
