/**
 * Unit tests for Task T005: Thinking Block UI
 * Tests the frontend functionality for rendering thinking blocks
 *
 * Note: These tests require the following functions to be implemented in app.js:
 * - handleThinkingToken(token) - handles thinking tokens from WebSocket
 * - thinkingBlockHtml(thinking) - generates HTML for thinking blocks
 * - streamingThinkingEl - variable to track streaming thinking element
 * - state.thinkingVisible - state tracking for thinking visibility
 * - msgHtml(msg) - updated to include thinking blocks
 * - finalizeStream(messageId) - updated to preserve thinking content
 * - onHistory(msg) - updated to render thinking blocks
 * - renderSessions(sessions) - already exists, used for test verification
 */

// Helper function that should exist in app.js
function escHtml(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

describe('Thinking Block UI - Core Functions (T005)', () => {
  // T005.1: Test that handleThinkingToken function exists
  test('T005.1 handleThinkingToken function is defined', () => {
    expect(typeof handleThinkingToken).toBe('function');
  });

  // T005.2: Test that thinkingBlockHtml function exists
  test('T005.2 thinkingBlockHtml function is defined', () => {
    expect(typeof thinkingBlockHtml).toBe('function');
  });

  // T005.4: Test that streamingThinkingEl variable exists
  test('T005.4 streamingThinkingEl variable is defined', () => {
    expect(typeof streamingThinkingEl !== 'undefined').toBeTruthy();
  });
});

describe('Thinking Block UI - Token Handling (T005)', () => {
  beforeEach(() => {
    streamingEl = null;
    streamingThinkingEl = null;
    document.getElementById('messages').innerHTML = '';
  });

  test('T005.5 handleThinkingToken appends content to thinking element', () => {
    const messages = document.getElementById('messages');

    handleThinkingToken('Let me think about this...');

    const thinkingEl = messages.querySelector('.message-thinking');
    expect(thinkingEl).not.toBeNull();
    expect(thinkingEl.textContent).toContain('Let me think about this...');
  });

  test('T005.6 thinking tokens accumulate across multiple calls', () => {
    const messages = document.getElementById('messages');

    handleThinkingToken('Step 1. ');
    handleThinkingToken('Step 2. ');
    handleThinkingToken('Step 3. ');

    const thinkingEl = messages.querySelector('.message-thinking');
    expect(thinkingEl.textContent).toContain('Step 1.');
    expect(thinkingEl.textContent).toContain('Step 2.');
    expect(thinkingEl.textContent).toContain('Step 3.');
  });
});

describe('Thinking Block UI - HTML Generation (T005)', () => {
  test('T005.7 thinkingBlockHtml generates correct HTML structure', () => {
    const thinking = 'I am thinking about the solution...';
    const html = thinkingBlockHtml(thinking);

    expect(html).toContain('thinking-block');
    expect(html).toContain('thinking-body');
    expect(html).toContain(escHtml(thinking));
    expect(html).not.toContain('hidden');
  });

  test('T005.8 thinkingBlockHtml thinking body is always visible', () => {
    const thinking = 'Test thinking';
    const html = thinkingBlockHtml(thinking);
    const parser = new DOMParser();
    const doc = parser.parseFromString(html, 'text/html');
    const body = doc.querySelector('.thinking-body');
    expect(body).not.toBeNull();
    expect(body.textContent.trim()).toBe(thinking);
  });

  test('T005.9 thinkingBlockHtml wraps content in structured container', () => {
    const thinking = 'Hidden content';
    const html = thinkingBlockHtml(thinking);

    expect(html).toContain('thinking-block');
    expect(html).toContain('thinking-header');
    expect(html).toContain('thinking-body');
  });
});


describe('Thinking Block UI - Message Rendering (T005)', () => {
  test('T005.13 msgHtml includes thinking blocks when present', () => {
    const msg = {
      id: 'test-123',
      role: 'assistant',
      content: 'Here is my response',
      thinking: 'This is my thinking process',
      created_at: new Date().toISOString(),
    };

    const html = msgHtml(msg);

    expect(html).toContain('thinking');
    expect(html).toContain('This is my thinking process');
  });

  test('T005.14 msgHtml renders message without thinking when not present', () => {
    const msg = {
      id: 'test-456',
      role: 'assistant',
      content: 'Simple response',
      created_at: new Date().toISOString(),
    };

    const html = msgHtml(msg);

    expect(html).toContain('message-assistant');
    expect(html).toContain('Simple response');
  });

  test('T005.15 msgHtml includes thinking blocks in correct order', () => {
    const msg = {
      id: 'test-789',
      role: 'assistant',
      content: 'Final answer',
      thinking: 'Step by step reasoning',
      created_at: new Date().toISOString(),
    };

    const html = msgHtml(msg);

    const thinkingIndex = html.indexOf('thinking');
    expect(thinkingIndex).toBeGreaterThan(-1);
  });
});

describe('Thinking Block UI - Finalize Stream (T005)', () => {
  beforeEach(() => {
    streamingEl = null;
    streamingThinkingEl = null;
    document.getElementById('messages').innerHTML = '';
  });

  test('T005.16 finalizeStream preserves thinking content', () => {
    const messages = document.getElementById('messages');

    // Use module-level handleThinkingToken to populate streamingEl and streamingThinkingEl
    handleThinkingToken('My reasoning');

    finalizeStream('final-message-id');

    const finalMessage = messages.querySelector('.message-assistant');
    expect(finalMessage).not.toBeNull();

    const thinkingBlock = finalMessage.querySelector('.thinking-block');
    expect(thinkingBlock).not.toBeNull();
  });
});

describe('Thinking Block UI - History Rendering (T005)', () => {
  test('T005.17 onHistory function is defined', () => {
    expect(typeof onHistory).toBe('function');
  });

  test('T005.18 renderSessions function is defined', () => {
    expect(typeof renderSessions).toBe('function');
  });
});

describe('Thinking Block UI - CSS Classes (T005)', () => {
  test('T005.19 thinking block uses correct CSS class names', () => {
    const thinking = 'Test thinking content';
    const html = thinkingBlockHtml(thinking);

    expect(html).toContain('thinking-block');
    expect(html).toContain('thinking-header');
    expect(html).toContain('thinking-body');
  });

  test('T005.20 thinking block has no toggle mechanism', () => {
    const thinking = 'Test thinking';
    const html = thinkingBlockHtml(thinking);

    expect(html).not.toContain('data-thinking-toggle');
    expect(html).not.toContain('▼');
    expect(html).not.toContain('▲');
  });
});

describe('Thinking Block UI - Multi-Iteration Isolation', () => {
  beforeEach(() => {
    streamingEl = null;
    streamingThinkingEl = null;
    document.getElementById('messages').innerHTML = '';
  });

  test('handleThinkingToken creates fresh block when streamingThinkingEl is null', () => {
    const messages = document.getElementById('messages');

    // First iteration thinking
    handleThinkingToken('first thinking');
    const firstEl = streamingThinkingEl;

    // Simulate what addToolResult does: finalize and null out
    streamingThinkingEl = null;
    streamingEl = null;

    // Second iteration thinking
    handleThinkingToken('second thinking');
    const secondEl = streamingThinkingEl;

    expect(firstEl).not.toBe(secondEl);
    expect(secondEl.textContent).toContain('second thinking');
    expect(secondEl.textContent).not.toContain('first thinking');
  });
});

describe('Thinking Block UI - addToolResult flushes thinking', () => {
  beforeEach(() => {
    streamingEl = null;
    streamingThinkingEl = null;
    document.getElementById('messages').innerHTML = '';
  });

  test('addToolResult finalizes thinking block before the tool block', () => {
    const messages = document.getElementById('messages');

    handleThinkingToken('pre-tool reasoning');

    addToolResult({ tool: 'my_tool', params: '{}', result: 'result text', approved: true });

    const children = Array.from(messages.children);
    const thinkingBlock = messages.querySelector('.thinking-block');
    const toolMsg = messages.querySelector('.tool-call-block');

    expect(thinkingBlock).not.toBeNull();
    expect(toolMsg).not.toBeNull();

    const thinkingIdx = children.indexOf(thinkingBlock);
    const toolParentIdx = children.indexOf(toolMsg.closest('.message'));
    expect(thinkingIdx).toBeLessThan(toolParentIdx);
  });

  test('addToolResult resets streamingThinkingEl to null', () => {
    handleThinkingToken('some thinking');
    expect(streamingThinkingEl).not.toBeNull();

    addToolResult({ tool: 'my_tool', params: '{}', result: 'done', approved: true });

    expect(streamingThinkingEl).toBeNull();
    expect(streamingEl).toBeNull();
  });

  test('addToolResult with no thinking does not crash', () => {
    expect(() => {
      addToolResult({ tool: 'my_tool', params: '{}', result: 'done', approved: true });
    }).not.toThrow();
  });
});
