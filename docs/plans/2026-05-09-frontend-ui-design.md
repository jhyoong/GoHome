# Frontend UI Design: Dark Mode Toggle + Shift+Enter Newline

Date: 2026-05-09

## Features

### 1. Dark Mode Toggle

**Goal:** Allow the user to switch between light and dark themes, persisting the preference across reloads. Default to dark mode when no preference is stored.

**CSS (`app.css`)**
- Extract all hardcoded color values into CSS custom properties on `:root` (light theme defaults).
- Add a `[data-theme="dark"]` block that overrides those variables. The dark palette reuses the existing sidebar colors (`#1e1e2e`, `#cdd6f4`, `#89b4fa`, etc.).

**HTML (`index.html`)**
- Add `<button id="theme-toggle">` in `.sidebar-header`, beside the "New Chat" button.
- Label reads "Dark" or "Light" to indicate the mode that clicking will switch to.

**JS (`app.js`)**
- On `DOMContentLoaded`, read `localStorage.getItem('theme')`. If absent, default to `"dark"`.
- Apply as `document.documentElement.dataset.theme`.
- Toggle button flips between `"light"` and `"dark"`, updates the attribute, saves to `localStorage`, and updates its own label.

---

### 2. Shift+Enter for Newline (Textarea Input)

**Goal:** Allow multi-line input. Enter submits; Shift+Enter inserts a newline. Textarea grows up to 5 lines before scrolling.

**HTML (`index.html`)**
- Replace `<input id="input" type="text">` with `<textarea id="input" rows="1"></textarea>`.

**CSS (`app.css`)**
- Replace `.input-bar input` rules with `.input-bar textarea` equivalents.
- Set `resize: none`, `overflow-y: auto`, and an explicit `line-height` (e.g. `1.4`) for accurate height calculation.

**JS (`app.js`)**
- On every `input` event: reset height to `"auto"`, then set to `scrollHeight` in pixels, capped at `5 * lineHeight`.
- On `keydown`: if `Enter` without `Shift`, prevent default and submit the form instead.
- After sending, reset textarea height to its single-line default.

---

## Files Changed

- `web/static/index.html`
- `web/static/app.css`
- `web/static/app.js`
- `web/dist/` (mirror of static — updated in sync)
