// State
const state = {
  sessions: [],
  messages: [],
  busy: false,
  awaitingApproval: null,
};

function isChainedShellCommand(cmd) {
  let inSingle = false, inDouble = false;
  for (let i = 0; i < cmd.length; i++) {
    const ch = cmd[i];
    if (ch === "'" && !inDouble) { inSingle = !inSingle; continue; }
    if (ch === '"' && !inSingle) { inDouble = !inDouble; continue; }
    if (inSingle || inDouble) continue;
    if (ch === '|' || ch === ';') return true;
    if (ch === '&' && i + 1 < cmd.length && cmd[i + 1] === '&') return true;
  }
  return false;
}

function suggestPattern(cmd) {
  const base = cmd.trim().split(/\s+/)[0] || cmd.trim();
  return base ? base + ' *' : '*';
}

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
  setBusy(false);
  dom.messages.innerHTML = state.messages.map(msgHtml).join('');
  scrollToBottom();
  renderSessions(state.sessions);
}

function msgHtml(msg) {
  const toolBlocks = (msg.tool_results || []).map(toolCallBlockHtml).join('');
  const text = (msg.content || '').trim();
  const content = text ? `<div class="message-content">${escHtml(text)}</div>` : '';
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
    const content = streamingEl.querySelector('.message-content').textContent.trim();
    if (content) {
      const msg = {
        id: messageId || crypto.randomUUID(),
        role: 'assistant',
        content,
        created_at: new Date().toISOString(),
      };
      state.messages.push(msg);
      const wrapper = document.createElement('div');
      wrapper.innerHTML = msgHtml(msg);
      streamingEl.replaceWith(wrapper.firstElementChild);
    } else {
      streamingEl.remove();
    }
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
  // Flush any streaming content before the tool result so order is: [preamble] → [tool] → [response]
  if (streamingEl) {
    const preamble = streamingEl.querySelector('.message-content').textContent.trim();
    if (preamble) {
      const prevMsg = { id: crypto.randomUUID(), role: 'assistant', content: preamble, created_at: new Date().toISOString() };
      state.messages.push(prevMsg);
      const w = document.createElement('div');
      w.innerHTML = msgHtml(prevMsg);
      streamingEl.replaceWith(w.firstElementChild);
    } else {
      streamingEl.remove();
    }
    streamingEl = null;
  }

  const tr = {
    id: crypto.randomUUID(),
    tool_name: msg.tool,
    params: msg.params,
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
  const params = msg.params || {};
  const shellCmd = msg.tool === 'shell' ? (params.command || '') : '';
  const chained = msg.tool === 'shell' && isChainedShellCommand(shellCmd);

  state.awaitingApproval = { request_id: msg.request_id, tool: msg.tool, params };
  dom.approvalTool.textContent = msg.tool;
  dom.approvalParams.textContent = JSON.stringify(params, null, 2);
  dom.approvalAlwaysAllow.hidden = chained;
  dom.alwaysAllowEditor.hidden = true;
  dom.approvalMainButtons.hidden = false;
  dom.approvalModal.hidden = false;
  dom.input.disabled = true;
  dom.sendBtn.disabled = true;
}

function hideApprovalModal() {
  state.awaitingApproval = null;
  dom.approvalModal.hidden = true;
  dom.alwaysAllowEditor.hidden = true;
  dom.approvalMainButtons.hidden = false;
  dom.input.disabled = false;
  setBusy(state.busy);
}

// ---- Helpers ----

function escHtml(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function formatJSON(v) {
  if (typeof v === 'string') {
    try { return JSON.stringify(JSON.parse(v), null, 2); } catch { return v; }
  }
  return JSON.stringify(v, null, 2);
}

function scrollToBottom() {
  dom.messages.scrollTop = dom.messages.scrollHeight;
}

// ---- Boot ----

document.addEventListener('DOMContentLoaded', () => {
  dom = {
    sessionList:    document.getElementById('session-list'),
    messages:       document.getElementById('messages'),
    input:          document.getElementById('input'),
    sendBtn:        document.getElementById('send-btn'),
    stopBtn:        document.getElementById('stop-btn'),
    newChatBtn:     document.getElementById('new-chat-btn'),
    errorBanner:    document.getElementById('error-banner'),
    errorText:      document.getElementById('error-text'),
    errorClose:     document.getElementById('error-close'),
    approvalModal:  document.getElementById('approval-modal'),
    approvalTool:   document.getElementById('approval-tool'),
    approvalParams: document.getElementById('approval-params'),
    approvalAllow:  document.getElementById('approval-allow'),
    approvalDeny:        document.getElementById('approval-deny'),
    approvalAlwaysAllow: document.getElementById('approval-always-allow'),
    alwaysAllowEditor:   document.getElementById('always-allow-editor'),
    alwaysAllowPattern:  document.getElementById('always-allow-pattern'),
    alwaysAllowConfirm:  document.getElementById('always-allow-confirm'),
    alwaysAllowCancel:   document.getElementById('always-allow-cancel'),
    approvalMainButtons: document.getElementById('approval-main-buttons'),
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
    if (!state.awaitingApproval) return;
    send({ type: 'tool_response', request_id: state.awaitingApproval.request_id, approved: true });
    hideApprovalModal();
  });

  dom.approvalDeny.addEventListener('click', () => {
    if (!state.awaitingApproval) return;
    send({ type: 'tool_response', request_id: state.awaitingApproval.request_id, approved: false });
    hideApprovalModal();
  });

  dom.approvalAlwaysAllow.addEventListener('click', () => {
    const a = state.awaitingApproval;
    if (!a) return;
    if (a.tool === 'shell') {
      dom.alwaysAllowPattern.value = suggestPattern(a.params.command || '');
      dom.alwaysAllowEditor.hidden = false;
      dom.approvalMainButtons.hidden = true;
    } else {
      send({ type: 'always_allow', request_id: a.request_id, tool: a.tool });
      hideApprovalModal();
    }
  });

  dom.alwaysAllowConfirm.addEventListener('click', () => {
    const a = state.awaitingApproval;
    if (!a) return;
    const pattern = dom.alwaysAllowPattern.value.trim();
    if (!pattern) return;
    send({ type: 'always_allow', request_id: a.request_id, tool: a.tool, command_pattern: pattern });
    hideApprovalModal();
  });

  dom.alwaysAllowCancel.addEventListener('click', () => {
    dom.alwaysAllowEditor.hidden = true;
    dom.approvalMainButtons.hidden = false;
  });

  connect();
});
