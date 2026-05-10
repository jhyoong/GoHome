// Test setup for Jest with jsdom environment
// This runs after the jest-environment-jsdom is initialized

// Mock WebSocket for testing
global.WebSocket = class {
  constructor() {}
  send() {}
  close() {}
  onopen = null;
  onmessage = null;
  onclose = null;
};

// Mock crypto.randomUUID
if (!global.crypto.randomUUID) {
  global.crypto.randomUUID = () => 'test-uuid-' + Math.random().toString(36).substr(2, 9);
}

// Ensure the DOM has all required elements
document.head = document.createElement('head');
document.body = document.createElement('body');

// Add required elements if they don't exist
const requiredIds = [
  'session-list',
  'messages',
  'input',
  'send-btn',
  'stop-btn',
  'new-chat-btn',
  'theme-toggle',
  'error-banner',
  'error-text',
  'error-close',
  'approval-modal',
  'approval-tool',
  'approval-params',
  'approval-allow',
  'approval-deny',
  'approval-always-allow',
  'approval-main-buttons',
  'always-allow-editor',
  'always-allow-pattern',
  'always-allow-confirm',
  'always-allow-cancel',
  'context-usage',
  'context-usage-text',
  'input-form',
];

requiredIds.forEach(id => {
  if (!document.getElementById(id)) {
    const el = document.createElement('div');
    el.id = id;
    if (id === 'input') {
      document.body.appendChild(document.createElement('textarea'));
      document.body.lastElementChild.id = id;
    } else if (id === 'send-btn' || id === 'stop-btn' || id === 'new-chat-btn' || id === 'theme-toggle' || id === 'error-close' || id === 'approval-allow' || id === 'approval-deny' || id === 'approval-always-allow' || id === 'always-allow-confirm' || id === 'always-allow-cancel') {
      document.body.appendChild(document.createElement('button'));
      document.body.lastElementChild.id = id;
    } else if (id === 'session-list' || id === 'messages') {
      document.body.appendChild(document.createElement('ul'));
      document.body.lastElementChild.id = id;
    } else {
      document.body.appendChild(el);
    }
  }
});

// Load and execute app.js to make its functions available globally
const fs = require('fs');
const path = require('path');
const appJsPath = path.join(__dirname, 'app.js');
const appJsCode = fs.readFileSync(appJsPath, 'utf-8');

// Execute app.js in the jsdom context
const script = document.createElement('script');
script.textContent = appJsCode;
(document.head || document.documentElement).appendChild(script);

// Manually populate dom object since DOMContentLoaded already fired
// With 'var dom' in app.js, this sets the global dom that app.js uses
dom = {
  sessionList:    document.getElementById('session-list'),
  messages:       document.getElementById('messages'),
  input:         document.getElementById('input'),
  sendBtn:       document.getElementById('send-btn'),
  stopBtn:       document.getElementById('stop-btn'),
  newChatBtn:    document.getElementById('new-chat-btn'),
  errorBanner:   document.getElementById('error-banner'),
  errorText:     document.getElementById('error-text'),
  errorClose:    document.getElementById('error-close'),
  approvalModal: document.getElementById('approval-modal'),
  approvalTool:  document.getElementById('approval-tool'),
  approvalParams: document.getElementById('approval-params'),
  approvalAllow: document.getElementById('approval-allow'),
  approvalDeny:  document.getElementById('approval-deny'),
  approvalAlwaysAllow: document.getElementById('approval-always-allow'),
  alwaysAllowEditor:  document.getElementById('always-allow-editor'),
  alwaysAllowPattern: document.getElementById('always-allow-pattern'),
  alwaysAllowConfirm: document.getElementById('always-allow-confirm'),
  alwaysAllowCancel:  document.getElementById('always-allow-cancel'),
  approvalMainButtons: document.getElementById('approval-main-buttons'),
  contextUsage:  document.getElementById('context-usage'),
  contextUsageText: document.getElementById('context-usage-text'),
  themeToggle:   document.getElementById('theme-toggle'),
  inputForm:     document.getElementById('input-form'),
};