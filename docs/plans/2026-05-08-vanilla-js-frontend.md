# Vanilla JS Frontend Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the Preact/TypeScript npm-based frontend with plain HTML + JS, eliminating all npm/npx dependencies and the build pipeline entirely.

**Architecture:** The Go binary embeds `web/static/` (raw files, no build step) instead of `web/dist/` (compiled output). `app.js` is a single ES module that manages WebSocket state and DOM updates directly. No bundler, no transpiler, no package manager.

**Tech Stack:** Vanilla JS (ES2020 modules), HTML5, CSS3, Go embed

---

### Task 1: Create web/static/ with index.html and app.css

**Files:**
- Create: `web/static/index.html`
- Create: `web/static/app.css` (copy of `web/src/app.css`)

**Step 1: Create the directory and HTML**

Create `web/static/index.html`:

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>Agent Chat</title>
  <link rel="stylesheet" href="app.css" />
</head>
<body>
  <div class="layout">
    <div class="sidebar">
      <div class="sidebar-header">
        <button id="new-chat-btn" class="btn-new">New Chat</button>
      </div>
      <ul id="session-list" class="session-list"></ul>
    </div>
    <main class="main">
      <div id="error-banner" class="error-banner" hidden>
        <span id="error-text"></span>
        <button id="error-close">×</button>
      </div>
      <div class="chat-view">
        <div id="messages" class="messages"></div>
        <form id="input-form" class="input-bar">
          <input id="input" type="text" placeholder="Type a message..." />
          <button id="stop-btn" type="button" class="btn-stop" hidden>Stop</button>
          <button id="send-btn" type="submit" disabled>Send</button>
        </form>
      </div>
      <div id="approval-modal" class="approval-modal" hidden>
        <div class="approval-card">
          <h3>Tool Approval Required</h3>
          <p>Tool: <code id="approval-tool"></code></p>
          <pre id="approval-params" class="params"></pre>
          <div class="approval-buttons">
            <button id="approval-allow" class="btn-allow">Allow</button>
            <button id="approval-deny" class="btn-deny">Deny</button>
          </div>
        </div>
      </div>
    </main>
  </div>
  <script src="app.js" type="module"></script>
</body>
</html>
```

**Step 2: Copy CSS**

```bash
cp web/src/app.css web/static/app.css
```

**Step 3: Commit**

```bash
git add web/static/index.html web/static/app.css
git commit -m "feat: add web/static scaffold (html + css)"
```

---

### Task 2: Write web/static/app.js

**Files:**
- Create: `web/static/app.js`

**Step 1: Write the file**

Create `web/static/app.js` with the full content below. Every dynamic value that goes into `innerHTML` is passed through `escHtml()` to prevent XSS from tool output or message content.

```js
// State
const state = {
  sessions: [],
  messages: [],
  busy: false,
  awaitingApproval: null,
};

let activeSessionId = null;
let ws = null;
let streamingEl = null;
const tabID = crypto.randomUUID();
let retryDelay = 1000;

// DOM refs — populated after DOMContentLoaded
let dom = {};

// ---- WebSocket ----

function send(msg) {
  ws?.send(JSON.stringify(msg));
}

function connect() {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  ws = new WebSocket(`${proto}//${location.host}/ws?tab=${tabID}`);

  ws.onopen = () => {
    retryDelay = 1000;
    if (activeSessionId) {
      send({ type: 'load_session', session_id: activeSessionId });
    } else {
      send({ type: 'new_session' });
    }
  };

  ws.onmessage = (e) => {
    const msg = JSON.parse(e.data);
    switch (msg.type) {
      case 'token':         appendToken(msg.data); break;
      case 'sessions':      renderSessions(msg.data); break;
      case 'history':       onHistory(msg); break;
      case 'tool_approval': showApprovalModal(msg); break;
      case 'tool_result':   addToolResult(msg); break;
      case 'done':          finalizeStream(msg.message_id); break;
      case 'stopped':       clearStream(); break;
      case 'error':         showError(msg.message); break;
    }
  };

  ws.onclose = () => {
    ws = null;
    setTimeout(() => {
      retryDelay = Math.min(retryDelay * 2, 30000);
      connect();
    }, retryDelay);
  };
}

// ---- Rendering ----

function renderSessions(sessions) {
  state.sessions = sessions;
  dom.sessionList.innerHTML = sessions.map(s => `
    <li class="${s.id === activeSessionId ? 'active' : ''}" data-id="${escHtml(s.id)}">
      <span class="session-title">${escHtml(s.title)}</span>
      <button class="btn-delete" data-delete="${escHtml(s.id)}">×</button>
    </li>
  `).join('');
}

function onHistory(msg) {
  activeSessionId = msg.session_id;
  state.messages = msg.messages;
  clearStreamEl();
  dom.messages.innerHTML = state.messages.map(msgHtml).join('');
  scrollToBottom();
  renderSessions(state.sessions);
}

function msgHtml(msg) {
  const toolBlocks = (msg.tool_results || []).map(toolCallBlockHtml).join('');
  const content = msg.content
    ? `<div class="message-content">${escHtml(msg.content)}</div>`
    : '';
  return `
    <div class="message message-${escHtml(msg.role)}">
      <div class="message-role">${escHtml(msg.role)}</div>
      ${content}${toolBlocks}
    </div>
  `;
}

function toolCallBlockHtml(tr) {
  const statusClass = tr.approved ? 'approved' : 'denied';
  const statusChar = tr.approved ? '✓' : '✗';
  return `
    <div class="tool-call-block">
      <button class="tool-call-header" data-tool-toggle>
        <span class="tool-call-status ${statusClass}">${statusChar}</span>
        <span class="tool-call-name">${escHtml(tr.tool_name)}</span>
        <span class="tool-call-toggle">▼</span>
      </button>
      <div class="tool-call-body" hidden>
        <div class="tool-call-label">Input</div>
        <pre class="tool-call-pre">${escHtml(formatJSON(tr.params))}</pre>
        <div class="tool-call-label">Output</div>
        <pre class="tool-call-pre">${escHtml(tr.result || '(empty)')}</pre>
      </div>
    </div>
  `;
}

// ---- Streaming ----

function appendToken(text) {
  if (!streamingEl) {
    streamingEl = document.createElement('div');
    streamingEl.className = 'message message-assistant';
    streamingEl.innerHTML = '<div class="message-role">assistant</div><div class="message-content"></div>';
    dom.messages.appendChild(streamingEl);
  }
  // textContent is safe — no HTML parsing
  streamingEl.querySelector('.message-content').textContent += text;
  scrollToBottom();
}

function finalizeStream(messageId) {
  if (streamingEl) {
    const content = streamingEl.querySelector('.message-content').textContent;
    state.messages.push({
      id: messageId || crypto.randomUUID(),
      role: 'assistant',
      content,
      created_at: new Date().toISOString(),
    });
    streamingEl = null;
  }
  setBusy(false);
}

function clearStream() {
  clearStreamEl();
  setBusy(false);
}

function clearStreamEl() {
  if (streamingEl) {
    streamingEl.remove();
    streamingEl = null;
  }
}

function addToolResult(msg) {
  const tr = {
    id: crypto.randomUUID(),
    tool_name: msg.tool,
    params: JSON.stringify(msg.params),
    result: msg.result,
    approved: msg.approved,
  };
  const syntheticMsg = {
    id: crypto.randomUUID(),
    role: 'assistant',
    content: '',
    tool_results: [tr],
    created_at: new Date().toISOString(),
  };
  state.messages.push(syntheticMsg);
  const wrapper = document.createElement('div');
  wrapper.innerHTML = msgHtml(syntheticMsg);
  dom.messages.appendChild(wrapper.firstElementChild);
  scrollToBottom();
}

// ---- UI state ----

function setBusy(busy) {
  state.busy = busy;
  dom.stopBtn.hidden = !busy;
  dom.sendBtn.disabled = busy || !dom.input.value.trim() || !!state.awaitingApproval;
  dom.input.placeholder = busy ? 'Agent running — type to steer...' : 'Type a message...';
}

function showError(text) {
  dom.errorText.textContent = text;
  dom.errorBanner.hidden = false;
  setBusy(false);
}

function showApprovalModal(msg) {
  state.awaitingApproval = { request_id: msg.request_id, tool: msg.tool, params: msg.params };
  dom.approvalTool.textContent = msg.tool;
  dom.approvalParams.textContent = JSON.stringify(msg.params, null, 2);
  dom.approvalModal.hidden = false;
  dom.input.disabled = true;
  dom.sendBtn.disabled = true;
}

function hideApprovalModal() {
  state.awaitingApproval = null;
  dom.approvalModal.hidden = true;
  dom.input.disabled = false;
  setBusy(state.busy);
}

// ---- Helpers ----

function escHtml(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

function formatJSON(s) {
  try { return JSON.stringify(JSON.parse(s), null, 2); } catch { return s; }
}

function scrollToBottom() {
  dom.messages.scrollTop = dom.messages.scrollHeight;
}

// ---- Boot ----

document.addEventListener('DOMContentLoaded', () => {
  dom = {
    sessionList:   document.getElementById('session-list'),
    messages:      document.getElementById('messages'),
    input:         document.getElementById('input'),
    sendBtn:       document.getElementById('send-btn'),
    stopBtn:       document.getElementById('stop-btn'),
    newChatBtn:    document.getElementById('new-chat-btn'),
    errorBanner:   document.getElementById('error-banner'),
    errorText:     document.getElementById('error-text'),
    errorClose:    document.getElementById('error-close'),
    approvalModal: document.getElementById('approval-modal'),
    approvalTool:  document.getElementById('approval-tool'),
    approvalParams:document.getElementById('approval-params'),
    approvalAllow: document.getElementById('approval-allow'),
    approvalDeny:  document.getElementById('approval-deny'),
  };

  document.getElementById('input-form').addEventListener('submit', (e) => {
    e.preventDefault();
    const content = dom.input.value.trim();
    if (!content || state.awaitingApproval) return;
    const userMsg = {
      id: crypto.randomUUID(),
      role: 'user',
      content,
      created_at: new Date().toISOString(),
    };
    state.messages.push(userMsg);
    const wrapper = document.createElement('div');
    wrapper.innerHTML = msgHtml(userMsg);
    dom.messages.appendChild(wrapper.firstElementChild);
    scrollToBottom();
    dom.input.value = '';
    setBusy(true);
    send({ type: 'message', session_id: activeSessionId, content });
  });

  dom.input.addEventListener('input', () => {
    dom.sendBtn.disabled = state.busy || !dom.input.value.trim() || !!state.awaitingApproval;
  });

  dom.stopBtn.addEventListener('click', () => send({ type: 'stop' }));

  dom.newChatBtn.addEventListener('click', () => send({ type: 'new_session' }));

  // Session list — event delegation
  dom.sessionList.addEventListener('click', (e) => {
    const deleteBtn = e.target.closest('[data-delete]');
    if (deleteBtn) {
      e.stopPropagation();
      send({ type: 'delete_session', session_id: deleteBtn.dataset.delete });
      return;
    }
    const li = e.target.closest('li[data-id]');
    if (li) send({ type: 'load_session', session_id: li.dataset.id });
  });

  // Tool call expand/collapse — event delegation
  dom.messages.addEventListener('click', (e) => {
    const header = e.target.closest('[data-tool-toggle]');
    if (!header) return;
    const body = header.closest('.tool-call-block').querySelector('.tool-call-body');
    const toggle = header.querySelector('.tool-call-toggle');
    const nowExpanded = body.hidden;
    body.hidden = !nowExpanded;
    toggle.textContent = nowExpanded ? '▲' : '▼';
  });

  dom.errorClose.addEventListener('click', () => { dom.errorBanner.hidden = true; });

  dom.approvalAllow.addEventListener('click', () => {
    send({ type: 'tool_response', request_id: state.awaitingApproval.request_id, approved: true });
    hideApprovalModal();
  });

  dom.approvalDeny.addEventListener('click', () => {
    send({ type: 'tool_response', request_id: state.awaitingApproval.request_id, approved: false });
    hideApprovalModal();
  });

  connect();
});
```

**Step 2: Commit**

```bash
git add web/static/app.js
git commit -m "feat: vanilla JS app logic (websocket, rendering, state)"
```

---

### Task 3: Update embed.go and cmd/agent/main.go

**Files:**
- Modify: `embed.go`
- Modify: `cmd/agent/main.go:114`

**Step 1: Update embed.go**

Change `embed.go` from:
```go
//go:embed web/dist
var WebDist embed.FS
```

To:
```go
//go:embed web/static
var WebStatic embed.FS
```

**Step 2: Update main.go**

In `cmd/agent/main.go`, change line 114:
```go
staticFS, err := fs.Sub(gohome.WebDist, "web/dist")
```

To:
```go
staticFS, err := fs.Sub(gohome.WebStatic, "web/static")
```

**Step 3: Verify it builds**

```bash
go build ./...
```

Expected: no errors.

**Step 4: Commit**

```bash
git add embed.go cmd/agent/main.go
git commit -m "feat: embed web/static instead of web/dist"
```

---

### Task 4: Update Makefile

**Files:**
- Modify: `Makefile`

**Step 1: Rewrite the Makefile**

Replace the full content of `Makefile` with:

```makefile
.PHONY: build run clean test

build:
	go build -o agent-chat ./cmd/agent

run: build
	./agent-chat

test:
	go test ./...

clean:
	rm -f agent-chat
```

**Step 2: Verify build still works**

```bash
make build
```

Expected: `agent-chat` binary produced, no npm/npx invoked.

**Step 3: Commit**

```bash
git add Makefile
git commit -m "chore: remove frontend build step from Makefile"
```

---

### Task 5: Delete old npm and TSX files

**Files:**
- Delete: `web/src/` (entire directory)
- Delete: `web/package.json`
- Delete: `web/package-lock.json`
- Delete: `web/node_modules/` (if present)

**Step 1: Remove the files**

```bash
rm -rf web/src web/package.json web/package-lock.json web/node_modules
```

**Step 2: Verify the build still works**

```bash
make build
```

Expected: succeeds. The Go embed directive now points to `web/static/` only, so deleting `web/src/` has no effect on the build.

**Step 3: Commit**

```bash
git add -A
git commit -m "chore: delete npm build pipeline and TSX source files"
```

---

### Task 6: Smoke test end-to-end

**Step 1: Start the server**

```bash
./agent-chat
```

Expected: `agent-chat listening on http://127.0.0.1:3000`

**Step 2: Open the browser**

Navigate to `http://127.0.0.1:3000`. Check:

- Sidebar renders with a "New Chat" button
- Sending a message delivers it over WebSocket and streams a response
- Tool call blocks are collapsible
- Tool approval modal appears when a tool requires approval
- Stop button appears while the agent is running
- Sessions list updates and switching sessions loads history
- Reconnect: kill and restart the server — the page should reconnect automatically

**Step 3: If anything is broken, debug before committing**

Check browser devtools console for JS errors. All DOM element lookups happen after `DOMContentLoaded`, so a missing `id` attribute in the HTML is the most common failure mode.
