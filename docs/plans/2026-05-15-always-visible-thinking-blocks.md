# Always-Visible Thinking Blocks Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove the collapse/expand toggle from thinking blocks so they are always visible with a "Thinking" label and italic text.

**Architecture:** Three-part change. Tests first: update/delete toggle-related tests. JS: replace the `<button>` in `thinkingBlockHtml` with a plain `<div>` and remove the delegated click handler branch. CSS: remove interactive styles, add italic to body.

**Tech Stack:** Vanilla JS (frontend), Jest/jsdom (frontend tests), CSS

---

### Task 1: Update tests to reflect always-visible thinking blocks

**Files:**
- Modify: `web/static/test_thinking_test.js`

**Background**

Six tests assert on toggle behavior that will no longer exist. Three describe toggle UI (T005.10, T005.11, T005.12) and must be deleted. Three assert on `data-thinking-toggle` or arrow icons (T005.7, T005.8, T005.20) and must be rewritten.

**Step 1: Delete the entire "Toggle Behavior" describe block**

In `web/static/test_thinking_test.js`, remove the full block from line 106 to line 208:

```
describe('Thinking Block UI - Toggle Behavior (T005)', () => {
  ...  (T005.10, T005.11, T005.12)
});
```

**Step 2: Rewrite T005.7**

Replace:
```js
  test('T005.7 thinkingBlockHtml generates correct HTML structure', () => {
    const thinking = 'I am thinking about the solution...';
    const html = thinkingBlockHtml(thinking);

    expect(html).toContain('data-thinking-toggle');
    expect(html).toContain('thinking');
    expect(html).toContain(escHtml(thinking));
  });
```

With:
```js
  test('T005.7 thinkingBlockHtml generates correct HTML structure', () => {
    const thinking = 'I am thinking about the solution...';
    const html = thinkingBlockHtml(thinking);

    expect(html).toContain('thinking-block');
    expect(html).toContain('thinking-body');
    expect(html).toContain(escHtml(thinking));
    expect(html).not.toContain('hidden');
  });
```

**Step 3: Rewrite T005.8**

Replace:
```js
  test('T005.8 thinkingBlockHtml includes toggle button', () => {
    const thinking = 'Test thinking';
    const html = thinkingBlockHtml(thinking);

    expect(html).toMatch(/[▼▲]/);
  });
```

With:
```js
  test('T005.8 thinkingBlockHtml thinking body is always visible', () => {
    const thinking = 'Test thinking';
    const html = thinkingBlockHtml(thinking);

    expect(html).not.toContain('hidden');
    expect(html).not.toContain('data-thinking-toggle');
  });
```

**Step 4: Rewrite T005.20**

Replace:
```js
  test('T005.20 thinking toggle uses data attribute', () => {
    const thinking = 'Test thinking';
    const html = thinkingBlockHtml(thinking);

    expect(html).toContain('data-thinking-toggle');
  });
```

With:
```js
  test('T005.20 thinking block has no toggle mechanism', () => {
    const thinking = 'Test thinking';
    const html = thinkingBlockHtml(thinking);

    expect(html).not.toContain('data-thinking-toggle');
    expect(html).not.toContain('▼');
    expect(html).not.toContain('▲');
  });
```

**Step 5: Run tests to verify the updated tests fail**

```
cd web/static && npm test -- --no-coverage 2>&1 | grep -E "PASS|FAIL|T005\.7|T005\.8|T005\.20"
```

Expected: T005.7, T005.8, T005.20 FAIL (current HTML still has `data-thinking-toggle`, `hidden`, and arrow icons)

**Step 6: Commit**

```bash
git add web/static/test_thinking_test.js
git commit -m "test: update thinking block tests for always-visible behavior"
```

---

### Task 2: Rewrite thinkingBlockHtml in app.js

**Files:**
- Modify: `web/static/app.js:182–192`

**Step 1: Replace the function body**

In `web/static/app.js`, replace lines 182–192:

```js
// Before:
function thinkingBlockHtml(thinking) {
  return `
    <div class="thinking-block">
      <button class="thinking-header" data-thinking-toggle onclick="var body=this.parentElement.querySelector('.thinking-body');var toggle=this.querySelector('.thinking-toggle');if(body&&toggle){var nowExpanded=body.hidden;body.hidden=!nowExpanded;toggle.textContent=nowExpanded?'▲':'▼'}">
        <span class="thinking-label">Thinking</span>
        <span class="thinking-toggle">▼</span>
      </button>
      <div class="thinking-body" hidden>${escHtml(thinking)}</div>
    </div>
  `;
}
```

```js
// After:
function thinkingBlockHtml(thinking) {
  return `
    <div class="thinking-block">
      <div class="thinking-header">
        <span class="thinking-label">Thinking</span>
      </div>
      <div class="thinking-body">${escHtml(thinking)}</div>
    </div>
  `;
}
```

**Step 2: Run tests**

```
cd web/static && npm test -- --no-coverage 2>&1 | grep -E "PASS|FAIL|T005"
```

Expected: T005.7, T005.8, T005.20 now PASS. All other thinking tests still pass.

**Step 3: Commit**

```bash
git add web/static/app.js
git commit -m "fix: replace thinking block toggle button with always-visible div"
```

---

### Task 3: Remove thinkingHeader branch from the click event listener

**Files:**
- Modify: `web/static/app.js:536–546`

**Step 1: Remove the dead handler branch**

In `web/static/app.js`, remove lines 536–546:

```js
// Remove this entire block:
    const thinkingHeader = e.target.closest('[data-thinking-toggle]');
    if (thinkingHeader) {
      const thinkingBlock = thinkingHeader.closest('.thinking-block');
      if (!thinkingBlock) return;
      const body = thinkingBlock.querySelector('.thinking-body');
      const toggle = thinkingHeader.querySelector('.thinking-toggle');
      if (!body || !toggle) return;
      const nowExpanded = body.hidden;
      body.hidden = !nowExpanded;
      toggle.textContent = nowExpanded ? '▲' : '▼';
    }
```

The surrounding `dom.messages.addEventListener('click', ...)` block and the tool-call toggle handler stay unchanged.

**Step 2: Run full test suite**

```
cd web/static && npm test -- --no-coverage
```

Expected: all tests PASS

**Step 3: Commit**

```bash
git add web/static/app.js
git commit -m "fix: remove thinking toggle click handler (always visible now)"
```

---

### Task 4: Update CSS for always-visible thinking blocks

**Files:**
- Modify: `web/static/app.css:325–359`

**Step 1: Update `.thinking-header` — remove interactive styles**

Replace lines 325–335:
```css
/* Before: */
.thinking-header {
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
```

```css
/* After: */
.thinking-header {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  background: var(--color-surface-alt);
}
```

**Step 2: Remove `.thinking-header:hover` rule entirely**

Delete lines 337–339:
```css
.thinking-header:hover {
  background: var(--color-surface-hover);
}
```

**Step 3: Remove `.thinking-toggle` rule entirely**

Delete lines 349–352:
```css
.thinking-toggle {
  color: var(--color-muted-mid);
  font-size: 10px;
}
```

**Step 4: Add `font-style: italic` to `.thinking-body`**

Replace lines 354–359:
```css
/* Before: */
.thinking-body {
  padding: 8px 10px;
  background: var(--color-surface-faint);
  white-space: pre-wrap;
  word-break: break-word;
}
```

```css
/* After: */
.thinking-body {
  padding: 8px 10px;
  background: var(--color-surface-faint);
  white-space: pre-wrap;
  word-break: break-word;
  font-style: italic;
}
```

**Step 5: Run full test suite one final time**

```
cd web/static && npm test -- --no-coverage
```

Expected: all tests PASS

**Step 6: Commit**

```bash
git add web/static/app.css
git commit -m "style: remove thinking toggle styles, add italic to thinking body"
```
