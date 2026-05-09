# Frontend UI: Dark Mode Toggle + Shift+Enter Textarea — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a persistent dark mode toggle to the sidebar and replace the single-line text input with an auto-growing textarea that submits on Enter and inserts a newline on Shift+Enter.

**Architecture:** All changes are in plain HTML/CSS/JS with no build step. CSS custom properties drive theming; a `data-theme` attribute on `<html>` selects the active palette. The textarea auto-resizes via `scrollHeight` clamped to 5 lines. Only `web/static/` is embedded by the Go server (`embed.go`) — `web/dist/` is ignored.

**Tech Stack:** Vanilla HTML5, CSS custom properties, ES modules (no framework, no bundler)

---

### Task 1: CSS — Introduce color variables and dark theme

**Files:**
- Modify: `web/static/app.css`

**Step 1: Replace the `:root` block and add the dark theme override block**

Find the existing `:root` block (line 4) and replace it with:

```css
:root {
  --input-bar-height: 57px; /* updated dynamically by JS when textarea grows */

  /* Light theme */
  --color-bg: #f5f5f5;
  --color-fg: #1a1a1a;
  --color-surface: #fff;
  --color-surface-alt: #f5f5f5;
  --color-surface-faint: #fafafa;
  --color-surface-hover: #ebebeb;
  --color-border: #e0e0e0;
  --color-input-border: #ccc;
  --color-code-bg: #f0f0f0;
  --color-muted: #888;
  --color-muted-mid: #666;
  --color-muted-dark: #555;
  --color-shadow: rgba(0, 0, 0, 0.2);
  --color-approved: #40a02b;
  --color-denied: #d20f39;
}

[data-theme="dark"] {
  --color-bg: #181825;
  --color-fg: #cdd6f4;
  --color-surface: #1e1e2e;
  --color-surface-alt: #181825;
  --color-surface-faint: #181825;
  --color-surface-hover: #45475a;
  --color-border: #45475a;
  --color-input-border: #585b70;
  --color-code-bg: #11111b;
  --color-muted: #6c7086;
  --color-muted-mid: #585b70;
  --color-muted-dark: #6c7086;
  --color-shadow: rgba(0, 0, 0, 0.5);
  --color-approved: #a6e3a1;
  --color-denied: #f38ba8;
}
```

**Step 2: Replace hardcoded color values in all rules with the new variables**

Apply these substitutions throughout the file. The sidebar, button accent colors (`#89b4fa`, `#f38ba8`, `#a6e3a1`, `#1e1e2e`), and status-icon accent colors stay hardcoded since they don't change between themes.

Full updated rules (only the ones that change):

```css
body {
  font-family: system-ui, -apple-system, sans-serif;
  font-size: 14px;
  background: var(--color-bg);
  color: var(--color-fg);
  height: 100vh;
  overflow: hidden;
}
```

```css
.message-assistant {
  align-self: flex-start;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
}
```

```css
.input-bar {
  display: flex;
  gap: 8px;
  padding: 12px 16px;
  border-top: 1px solid var(--color-border);
  background: var(--color-surface);
}
```

```css
.input-bar input {
  flex: 1;
  padding: 8px 12px;
  border: 1px solid var(--color-input-border);
  border-radius: 6px;
  font-size: 14px;
  outline: none;
}
```

```css
.approval-card {
  background: var(--color-surface);
  border-radius: 10px;
  padding: 24px;
  box-shadow: 0 8px 32px var(--color-shadow);
  max-height: calc(100vh - var(--input-bar-height) - 24px);
  overflow-y: auto;
}

.approval-card code { background: var(--color-code-bg); padding: 2px 6px; border-radius: 4px; }
```

```css
.params {
  background: var(--color-surface-alt);
  padding: 12px;
  border-radius: 6px;
  margin-bottom: 16px;
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-word;
}
```

```css
.tool-call-block {
  margin-top: 8px;
  border: 1px solid var(--color-border);
  border-radius: 6px;
  overflow: hidden;
  font-size: 12px;
}
```

```css
.tool-call-header {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  background: var(--color-surface-alt);
  border: none;
  cursor: pointer;
  text-align: left;
}

.tool-call-header:hover { background: var(--color-surface-hover); }
```

```css
.tool-call-status.approved { color: var(--color-approved); font-weight: bold; }
.tool-call-status.denied { color: var(--color-denied); font-weight: bold; }
```

```css
.tool-call-toggle { color: var(--color-muted-mid); font-size: 10px; }
```

```css
.tool-call-body { padding: 8px 10px; background: var(--color-surface-faint); }
```

```css
.tool-call-label {
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  color: var(--color-muted);
  margin-bottom: 4px;
  margin-top: 8px;
}
```

```css
.tool-call-pre {
  background: var(--color-code-bg);
  padding: 6px 8px;
  border-radius: 4px;
  overflow: auto;
  max-height: 200px;
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 11px;
}
```

```css
.pattern-label {
  display: block;
  font-size: 12px;
  font-weight: 600;
  color: var(--color-muted-dark);
  margin-bottom: 6px;
}
```

```css
.pattern-input {
  width: 100%;
  padding: 8px 10px;
  border: 1px solid var(--color-input-border);
  border-radius: 6px;
  font-family: monospace;
  font-size: 13px;
  margin-bottom: 4px;
}
```

```css
.context-usage {
  text-align: right;
  padding: 2px 8px 4px;
  font-size: 0.72rem;
  color: var(--color-muted);
  user-select: none;
}
```

**Step 3: Add `.btn-theme` style** (add at the end of the file, before `.context-usage`)

```css
.btn-theme {
  padding: 5px 10px;
  background: #313244;
  color: #cdd6f4;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  font-size: 12px;
}
```

**Step 4: Visual check**

Open `http://localhost:8080` in a browser (run `make run` first). The page should look identical to before — no visual change yet since `data-theme` is not set.

**Step 5: Commit**

```bash
git add web/static/app.css
git commit -m "feat: introduce CSS custom properties for theme colors"
```

---

### Task 2: HTML — Add dark mode toggle button

**Files:**
- Modify: `web/static/index.html`

**Step 1: Replace `.sidebar-header` contents**

Find:
```html
    <div class="sidebar-header">
      <button id="new-chat-btn" class="btn-new">New Chat</button>
    </div>
```

Replace with:
```html
    <div class="sidebar-header">
      <button id="new-chat-btn" class="btn-new">New Chat</button>
      <button id="theme-toggle" class="btn-theme" style="margin-top:8px">Light</button>
    </div>
```

The label "Light" means "click to switch to light mode" — JS will set the correct initial label.

**Step 2: Visual check**

Reload the page. A small "Light" button should appear below "New Chat" in the sidebar. Clicking it does nothing yet.

**Step 3: Commit**

```bash
git add web/static/index.html
git commit -m "feat: add dark mode toggle button to sidebar"
```

---

### Task 3: JS — Implement dark mode logic

**Files:**
- Modify: `web/static/app.js`

**Step 1: Add `themeToggle` to the `dom` object**

In the `DOMContentLoaded` handler, inside the `dom = { ... }` block, add after `contextUsageText`:

```js
    themeToggle: document.getElementById('theme-toggle'),
```

**Step 2: Add `applyTheme` function** (add near the top of the file, after the `state` object)

```js
function applyTheme(theme) {
  document.documentElement.dataset.theme = theme;
  localStorage.setItem('theme', theme);
  if (dom.themeToggle) {
    dom.themeToggle.textContent = theme === 'dark' ? 'Light' : 'Dark';
  }
}
```

**Step 3: Initialize theme on load**

At the end of the `DOMContentLoaded` handler, just before the `connect()` call, add:

```js
  const savedTheme = localStorage.getItem('theme') ?? 'dark';
  applyTheme(savedTheme);

  dom.themeToggle.addEventListener('click', () => {
    const next = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
    applyTheme(next);
  });
```

**Step 4: Visual check**

Reload the page. It should load in dark mode by default. Click the toggle — it should switch to light mode and the button label should update. Refresh — the last chosen theme should persist. Check that all elements (messages, approval modal, tool call blocks, input bar) respond correctly to the theme switch.

**Step 5: Commit**

```bash
git add web/static/app.js
git commit -m "feat: implement dark mode toggle with localStorage persistence"
```

---

### Task 4: HTML — Replace text input with textarea

**Files:**
- Modify: `web/static/index.html`

**Step 1: Replace the `<input>` element**

Find:
```html
          <input id="input" type="text" placeholder="Type a message..." aria-label="Message" />
```

Replace with:
```html
          <textarea id="input" rows="1" placeholder="Type a message..." aria-label="Message"></textarea>
```

**Step 2: Visual check**

Reload the page. The input area should look similar but slightly taller (one row textarea). Typing should work. Pressing Enter will insert a newline for now — submit is wired in Task 6.

**Step 3: Commit**

```bash
git add web/static/index.html
git commit -m "feat: replace text input with textarea for multi-line support"
```

---

### Task 5: CSS — Style the textarea

**Files:**
- Modify: `web/static/app.css`

**Step 1: Replace `.input-bar input` and `.input-bar input:focus` rules**

Find:
```css
.input-bar input {
  flex: 1;
  padding: 8px 12px;
  border: 1px solid var(--color-input-border);
  border-radius: 6px;
  font-size: 14px;
  outline: none;
}

.input-bar input:focus { border-color: #89b4fa; }
```

Replace with:
```css
.input-bar textarea {
  flex: 1;
  padding: 8px 12px;
  border: 1px solid var(--color-input-border);
  border-radius: 6px;
  font-size: 14px;
  font-family: inherit;
  line-height: 1.4;
  outline: none;
  resize: none;
  overflow-y: hidden;
  background: transparent;
  color: inherit;
}

.input-bar textarea:focus { border-color: #89b4fa; }
```

`background: transparent` and `color: inherit` ensure the textarea picks up the theme colors from the parent `.input-bar` rather than the browser's default white-on-white.

**Step 2: Visual check**

Reload. The textarea should look like the old input — single line, same padding, no resize handle. Typing should feel the same visually. In dark mode it should have the correct background and text color.

**Step 3: Commit**

```bash
git add web/static/app.css
git commit -m "feat: style textarea to match old input, inherit theme colors"
```

---

### Task 6: JS — Textarea auto-resize and keyboard handling

**Files:**
- Modify: `web/static/app.js`

**Step 1: Add `resizeTextarea` helper** (add after the `applyTheme` function)

```js
function resizeTextarea() {
  const el = dom.input;
  const style = window.getComputedStyle(el);
  const lineHeight = parseFloat(style.lineHeight);
  const paddingTop = parseFloat(style.paddingTop);
  const paddingBottom = parseFloat(style.paddingBottom);
  const maxHeight = lineHeight * 5 + paddingTop + paddingBottom;

  el.style.height = 'auto';
  const next = Math.min(el.scrollHeight, maxHeight);
  el.style.height = next + 'px';
  el.style.overflowY = el.scrollHeight > maxHeight ? 'auto' : 'hidden';

  document.documentElement.style.setProperty(
    '--input-bar-height',
    dom.inputForm.offsetHeight + 'px'
  );
}
```

`resizeTextarea` resets the height to `auto` first so `scrollHeight` reflects the true content height, then clamps to 5 lines. It also updates `--input-bar-height` so the approval modal stays correctly positioned as the textarea grows.

**Step 2: Add `inputForm` to the `dom` object**

In the `dom = { ... }` block, add:

```js
    inputForm: document.getElementById('input-form'),
```

**Step 3: Replace the existing `dom.input` `input` event listener**

Find:
```js
  dom.input.addEventListener('input', () => {
    dom.sendBtn.disabled = state.busy || !dom.input.value.trim() || !!state.awaitingApproval;
  });
```

Replace with:
```js
  dom.input.addEventListener('input', () => {
    dom.sendBtn.disabled = state.busy || !dom.input.value.trim() || !!state.awaitingApproval;
    resizeTextarea();
  });
```

**Step 4: Add `keydown` listener on the textarea** (add immediately after the `input` listener above)

```js
  dom.input.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      dom.inputForm.requestSubmit();
    }
  });
```

`requestSubmit()` fires the `submit` event on the form, which triggers the existing submit handler, including the `awaitingApproval` guard.

**Step 5: Reset textarea height after sending**

In the form `submit` handler, find where `dom.input.value = ''` is set (after `send(...)`) and add the height reset immediately after:

```js
    dom.input.value = '';
    dom.input.style.height = '';
    dom.input.style.overflowY = 'hidden';
    document.documentElement.style.setProperty('--input-bar-height', '57px');
```

**Step 6: Visual check**

Reload. Verify:
- Typing a short message and pressing Enter submits it (no newline added).
- Pressing Shift+Enter inserts a newline and the textarea grows.
- After 5 lines the textarea stops growing and gains a scrollbar.
- After sending, the textarea snaps back to a single line.
- The approval modal (trigger an agent run that needs approval) stays properly above the input bar when the textarea is tall.
- Dark mode still applies correctly to the textarea.

**Step 7: Commit**

```bash
git add web/static/app.js
git commit -m "feat: auto-resize textarea up to 5 lines, Enter submits, Shift+Enter newlines"
```
