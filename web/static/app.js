// State
const state = {
  sessions: [],
  messages: [],
  busy: false,
  awaitingApproval: null,
};

function applyTheme(theme) {
  document.documentElement.dataset.theme = theme;
  localStorage.setItem('theme', theme);
  dom.themeToggle.textContent = theme === 'dark' ? 'Light' : 'Dark';
}

function resizeTextarea() {
  const el = dom.input;
  if (!el) return;
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
let streamingThinkingEl = null;

// Maps subagent session_id -> { blockEl, bodyEl, tokenEl, thinkingEl }
const subagentBlocks = new Map();

function generateUUID() {
  if (crypto && crypto.randomUUID) {
    return crypto.randomUUID();
  }
  // Fallback for older browsers/safari
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
    const r = Math.random() * 16 | 0;
    const v = c === 'x' ? r : (r & 0x3 | 0x8);
    return v.toString(16);
  });
}

const tabID = generateUUID();
let retryDelay = 1000;

// DOM refs — populated after DOMContentLoaded (use global to allow test setup)
var dom = {};

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
      send({ type: 'list_sessions' });
    }
  };

  ws.onmessage = (e) => {
    const msg = JSON.parse(e.data);
    switch (msg.type) {
      case 'token':         appendToken(msg.data); break;
      case 'thinking_token': handleThinkingToken(msg.data); break;
      case 'sessions':      renderSessions(msg.data); break;
      case 'history':       onHistory(msg); break;
      case 'session_created':
        activeSessionId = msg.session_id;
        break;
      case 'tool_approval': showApprovalModal(msg); break;
      case 'tool_result':   addToolResult(msg); break;
      case 'done':          finalizeStream(msg.message_id); break;
      case 'stopped':       clearStream(); break;
      case 'error':         showError(msg.message); break;
      case 'usage':         updateContextUsage(msg); break;
      case 'subagent_start':         openSubagentBlock(msg.session_id, msg.data); break;
      case 'subagent_token':         appendSubagentToken(msg.session_id, msg.data); break;
      case 'subagent_thinking_token': appendSubagentThinkingToken(msg.session_id, msg.data); break;
      case 'subagent_tool_result':   addSubagentToolResult(msg); break;
      case 'subagent_done':          finalizeSubagentBlock(msg.session_id); break;
      case 'subagent_error':         errorSubagentBlock(msg.session_id, msg.message); break;
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
  dom.contextUsage.hidden = true;
  dom.contextUsageText.textContent = '';
}

function msgHtml(msg) {
  const toolBlocks = (msg.tool_results || []).map(toolCallBlockHtml).join('');
  const thinkingItems = Array.isArray(msg.thinking) ? msg.thinking : (msg.thinking ? [msg.thinking] : []);
  const thinkingBlocks = thinkingItems.map(thinkingBlockHtml).join('');
  const text = (msg.content || '').trim();
  const content = text ? `<div class="message-content">${escHtml(text)}</div>` : '';
  return `
    <div class="message message-${escHtml(msg.role)}">
      <div class="message-role">${escHtml(msg.role)}</div>
      ${thinkingBlocks}${content}${toolBlocks}
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

function handleThinkingToken(text) {
  if (!streamingThinkingEl) {
    const messageEl = document.createElement('div');
    messageEl.className = 'message message-assistant';
    messageEl.innerHTML = '<div class="message-role">assistant</div><div class="message-content"></div>';
    dom.messages.appendChild(messageEl);

    const thinkingWrapper = document.createElement('div');
    thinkingWrapper.innerHTML = thinkingBlockHtml('');
    const thinkingBlock = thinkingWrapper.firstElementChild;
    messageEl.insertBefore(thinkingBlock, messageEl.querySelector('.message-content'));

    streamingEl = messageEl;
    streamingThinkingEl = thinkingBlock.querySelector('.thinking-body');
  }

  streamingThinkingEl.textContent += text;
  scrollToBottom();
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
    const thinkingContent = streamingThinkingEl ? streamingThinkingEl.textContent.trim() : '';
    if (content || thinkingContent) {
      const msg = {
        id: messageId || generateUUID(),
        role: 'assistant',
        content,
        created_at: new Date().toISOString(),
      };
      if (thinkingContent) {
        msg.thinking = [thinkingContent];
      }
      state.messages.push(msg);
      const wrapper = document.createElement('div');
      wrapper.innerHTML = msgHtml(msg);
      streamingEl.replaceWith(wrapper.firstElementChild);
    } else {
      streamingEl.remove();
    }
    streamingEl = null;
    streamingThinkingEl = null;
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
    streamingThinkingEl = null;
  }
}

function addToolResult(msg) {
  // Finalize any streaming assistant turn (thinking + text) before adding tool result
  if (streamingEl) {
    const content = streamingEl.querySelector('.message-content').textContent.trim();
    const thinkingContent = streamingThinkingEl ? streamingThinkingEl.textContent.trim() : '';
    if (content || thinkingContent) {
      const prevMsg = { id: generateUUID(), role: 'assistant', content, created_at: new Date().toISOString() };
      if (thinkingContent) {
        prevMsg.thinking = [thinkingContent];
      }
      state.messages.push(prevMsg);
      const w = document.createElement('div');
      w.innerHTML = msgHtml(prevMsg);
      streamingEl.replaceWith(w.firstElementChild);
    } else {
      streamingEl.remove();
    }
    streamingEl = null;
    streamingThinkingEl = null;
  }

  const tr = {
    id: generateUUID(),
    tool_name: msg.tool,
    params: msg.params,
    result: msg.result,
    approved: msg.approved,
  };
  const syntheticMsg = {
    id: generateUUID(),
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
  dom.newChatBtn.disabled = busy;
  dom.sendBtn.disabled = busy || !dom.input.value.trim() || !!state.awaitingApproval;
  dom.input.placeholder = busy ? 'Agent running — type to steer...' : 'Type a message...';
}

function showError(text) {
  dom.errorText.textContent = text;
  dom.errorBanner.hidden = false;
  setBusy(false);
}

function updateContextUsage(msg) {
  if (!msg.context_window) return;
  const used = Math.round(msg.prompt_tokens / 1000);
  const max = Math.round(msg.context_window / 1000);
  const pct = Math.round((msg.prompt_tokens / msg.context_window) * 100);
  dom.contextUsageText.textContent = `${used}k / ${max}k (${pct}%)`;
  dom.contextUsage.hidden = false;
}

function handleApprovalKeys(e) {
  // Suspend when typing in any text field (pattern input or main textarea)
  if (document.activeElement === dom.alwaysAllowPattern) return;
  if (document.activeElement === dom.input) return;

  const buttons = dom.alwaysAllowEditor.hidden
    ? [dom.approvalAllow, dom.approvalAlwaysAllow, dom.approvalDeny].filter(btn => !btn.hidden)
    : [dom.alwaysAllowConfirm, dom.alwaysAllowCancel];

  const idx = buttons.indexOf(document.activeElement);

  if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
    // Don't wrap - stop at the last button
    const nextIdx = idx === -1 ? 0 : idx + 1;
    if (nextIdx < buttons.length) {
      e.preventDefault();
      buttons[nextIdx].focus();
    }
  } else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
    // Don't wrap - stop at the first button
    const nextIdx = idx - 1;
    if (nextIdx >= 0) {
      e.preventDefault();
      buttons[nextIdx].focus();
    }
  } else if (e.key === 'Escape') {
    e.preventDefault();
    if (!dom.alwaysAllowEditor.hidden) {
      dom.alwaysAllowCancel.click();
    } else {
      dom.approvalDeny.click();
    }
  }
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
  dom.input.blur();
  dom.input.disabled = true;
  dom.sendBtn.disabled = true;
  document.removeEventListener('keydown', handleApprovalKeys);
  document.addEventListener('keydown', handleApprovalKeys);
  requestAnimationFrame(() => dom.approvalAllow.focus());
}

function hideApprovalModal() {
  document.removeEventListener('keydown', handleApprovalKeys);
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

// ---- Subagent rendering ----

function openSubagentBlock(sessionID, parentID) {
  const blockEl = document.createElement('div');
  blockEl.className = 'subagent-block';
  blockEl.dataset.sessionId = sessionID;

  blockEl.innerHTML = `
    <button class="subagent-header" data-subagent-toggle>
      <span class="subagent-status running">&#9689;</span>
      <span class="subagent-label">Subagent</span>
      <span class="subagent-toggle">&#9660;</span>
    </button>
    <div class="subagent-body"></div>
  `;

  blockEl.querySelector('[data-subagent-toggle]').addEventListener('click', () => {
    const body = blockEl.querySelector('.subagent-body');
    const toggle = blockEl.querySelector('.subagent-toggle');
    const hidden = body.hidden;
    body.hidden = !hidden;
    toggle.textContent = hidden ? '▼' : '▶';
  });

  dom.messages.appendChild(blockEl);
  scrollToBottom();

  subagentBlocks.set(sessionID, {
    blockEl,
    bodyEl: blockEl.querySelector('.subagent-body'),
    tokenEl: null,
    thinkingEl: null,
  });
}

function appendSubagentToken(sessionID, token) {
  const entry = subagentBlocks.get(sessionID);
  if (!entry) return;
  if (!entry.tokenEl) {
    entry.tokenEl = document.createElement('div');
    entry.tokenEl.className = 'subagent-text';
    entry.bodyEl.appendChild(entry.tokenEl);
  }
  entry.tokenEl.textContent += token;
  scrollToBottom();
}

function appendSubagentThinkingToken(sessionID, token) {
  const entry = subagentBlocks.get(sessionID);
  if (!entry) return;
  if (!entry.thinkingEl) {
    const wrapper = document.createElement('div');
    wrapper.innerHTML = thinkingBlockHtml('');
    entry.bodyEl.insertBefore(wrapper.firstElementChild, entry.bodyEl.firstChild);
    entry.thinkingEl = entry.bodyEl.querySelector('.thinking-body');
  }
  entry.thinkingEl.textContent += token;
  scrollToBottom();
}

function addSubagentToolResult(msg) {
  const entry = subagentBlocks.get(msg.session_id);
  if (!entry) return;
  const tr = {
    tool_name: msg.tool,
    params: msg.params,
    result: msg.result,
    approved: msg.approved,
  };
  const wrapper = document.createElement('div');
  wrapper.innerHTML = toolCallBlockHtml(tr);
  const toolBlock = wrapper.firstElementChild;
  entry.bodyEl.appendChild(toolBlock);
  scrollToBottom();
}

function finalizeSubagentBlock(sessionID) {
  const entry = subagentBlocks.get(sessionID);
  if (!entry) return;
  const status = entry.blockEl.querySelector('.subagent-status');
  status.textContent = '✓';
  status.className = 'subagent-status done';
  subagentBlocks.delete(sessionID);
}

function errorSubagentBlock(sessionID, errMsg) {
  const entry = subagentBlocks.get(sessionID);
  if (!entry) return;
  const status = entry.blockEl.querySelector('.subagent-status');
  status.textContent = '✗';
  status.className = 'subagent-status error';
  if (errMsg) {
    const errEl = document.createElement('div');
    errEl.className = 'subagent-error-text';
    errEl.textContent = errMsg;
    entry.bodyEl.appendChild(errEl);
  }
  subagentBlocks.delete(sessionID);
}

// ---- Boot ----

// Extract init code into a function so it can be called immediately if document is ready
function initApp() {
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
    contextUsage:     document.getElementById('context-usage'),
    contextUsageText: document.getElementById('context-usage-text'),
    themeToggle:      document.getElementById('theme-toggle'),
    inputForm:        document.getElementById('input-form'),
  };

  document.getElementById('input-form').addEventListener('submit', (e) => {
    e.preventDefault();
    const content = dom.input.value.trim();
    if (!content || state.awaitingApproval) return;
    const userMsg = {
id: generateUUID(),
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
    dom.input.style.height = '';
    dom.input.style.overflowY = 'hidden';
    resizeTextarea();
    setBusy(true);
    send({ type: 'message', session_id: activeSessionId, content });
  });

  dom.input.addEventListener('input', () => {
    dom.sendBtn.disabled = state.busy || !dom.input.value.trim() || !!state.awaitingApproval;
    resizeTextarea();
  });

  dom.input.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      dom.inputForm.requestSubmit();
    }
  });

  dom.stopBtn.addEventListener('click', () => send({ type: 'stop' }));

  dom.newChatBtn.addEventListener('click', () => {
    activeSessionId = null;
    state.messages = [];
    dom.messages.innerHTML = '';
    dom.contextUsage.hidden = true;
    dom.contextUsageText.textContent = '';
    renderSessions(state.sessions);
  });

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
    const toolHeader = e.target.closest('[data-tool-toggle]');
    if (toolHeader) {
      const toolBlock = toolHeader.closest('.tool-call-block');
      if (!toolBlock) return;
      const body = toolBlock.querySelector('.tool-call-body');
      const toggle = toolHeader.querySelector('.tool-call-toggle');
      if (!body || !toggle) return;
      const nowExpanded = body.hidden;
      body.hidden = !nowExpanded;
      toggle.textContent = nowExpanded ? '▲' : '▼';
    }

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
      requestAnimationFrame(() => dom.alwaysAllowConfirm.focus());
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

  const savedTheme = localStorage.getItem('theme') ?? 'dark';
  applyTheme(savedTheme);

  dom.themeToggle.addEventListener('click', () => {
    const next = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
    applyTheme(next);
  });

  resizeTextarea();
  connect();
}

document.addEventListener('DOMContentLoaded', initApp);
