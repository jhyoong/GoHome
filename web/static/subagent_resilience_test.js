/**
 * Tests that the main-session UI tolerates a missing/dropped subagent_start
 * event. If subagent_start is lost (e.g., dropped by a full outbound channel
 * on the backend), the first subsequent subagent_* event for that session
 * must lazily create the block so the user still sees the subagent's activity.
 *
 * Also covers the addToolResult fallback for spawn_subagent when no subagent
 * block was rendered.
 */

describe('Subagent renderers — self-healing when subagent_start was lost', () => {
  beforeEach(() => {
    streamingEl = null;
    streamingThinkingEl = null;
    document.getElementById('messages').innerHTML = '';
    subagentBlocks.clear();
  });

  test('appendSubagentToken creates a block when no entry exists yet', () => {
    appendSubagentToken('child-1', 'hello from subagent');

    const block = document.querySelector('.subagent-block[data-session-id="child-1"]');
    expect(block).not.toBeNull();
    const textEl = block.querySelector('.subagent-text');
    expect(textEl).not.toBeNull();
    expect(textEl.textContent).toBe('hello from subagent');
  });

  test('appendSubagentThinkingToken creates a block when no entry exists yet', () => {
    appendSubagentThinkingToken('child-2', 'planning...');

    const block = document.querySelector('.subagent-block[data-session-id="child-2"]');
    expect(block).not.toBeNull();
    const thinking = block.querySelector('.thinking-body');
    expect(thinking).not.toBeNull();
    expect(thinking.textContent).toBe('planning...');
  });

  test('addSubagentToolResult creates a block when no entry exists yet', () => {
    addSubagentToolResult({
      session_id: 'child-3',
      tool: 'shell',
      params: { command: 'ls' },
      result: 'file.txt',
      approved: true,
    });

    const block = document.querySelector('.subagent-block[data-session-id="child-3"]');
    expect(block).not.toBeNull();
    const toolBlock = block.querySelector('.tool-call-block');
    expect(toolBlock).not.toBeNull();
    expect(toolBlock.querySelector('.tool-call-name').textContent).toBe('shell');
  });

  test('finalizeSubagentBlock creates a block then marks it done', () => {
    finalizeSubagentBlock('child-4');

    const block = document.querySelector('.subagent-block[data-session-id="child-4"]');
    expect(block).not.toBeNull();
    const status = block.querySelector('.subagent-status');
    expect(status.classList.contains('done')).toBe(true);
    expect(status.textContent).toBe('✓');
  });

  test('errorSubagentBlock creates a block then marks it errored', () => {
    errorSubagentBlock('child-5', 'something went wrong');

    const block = document.querySelector('.subagent-block[data-session-id="child-5"]');
    expect(block).not.toBeNull();
    const status = block.querySelector('.subagent-status');
    expect(status.classList.contains('error')).toBe(true);
    expect(status.textContent).toBe('✗');
    expect(block.querySelector('.subagent-error-text').textContent).toBe('something went wrong');
  });

  test('out-of-order: token, then tool_result, then done all populate one block', () => {
    appendSubagentToken('child-6', 'streaming text');
    addSubagentToolResult({
      session_id: 'child-6',
      tool: 'read_file',
      params: { path: 'a.txt' },
      result: 'contents',
      approved: true,
    });
    finalizeSubagentBlock('child-6');

    const blocks = document.querySelectorAll('.subagent-block[data-session-id="child-6"]');
    expect(blocks.length).toBe(1);
    const block = blocks[0];
    expect(block.querySelector('.subagent-text').textContent).toBe('streaming text');
    expect(block.querySelector('.tool-call-block')).not.toBeNull();
    expect(block.querySelector('.subagent-status').classList.contains('done')).toBe(true);
  });

  test('normal path still works: openSubagentBlock first, then token', () => {
    openSubagentBlock('child-7', 'parent-id', 'do the thing');
    appendSubagentToken('child-7', 'ok');

    const blocks = document.querySelectorAll('.subagent-block[data-session-id="child-7"]');
    expect(blocks.length).toBe(1);
    expect(blocks[0].querySelector('.subagent-text').textContent).toBe('ok');
    expect(blocks[0].querySelector('.subagent-task').textContent).toBe('do the thing');
  });
});

describe('addToolResult fallback for spawn_subagent', () => {
  beforeEach(() => {
    streamingEl = null;
    streamingThinkingEl = null;
    document.getElementById('messages').innerHTML = '';
    subagentBlocks.clear();
    state.messages = [];
  });

  test('renders a generic tool-call block when no subagent block exists', () => {
    addToolResult({
      tool: 'spawn_subagent',
      params: { task: 'explore docs' },
      result: 'summary text',
      approved: true,
    });

    const toolBlock = document.querySelector('.tool-call-block');
    expect(toolBlock).not.toBeNull();
    expect(toolBlock.querySelector('.tool-call-name').textContent).toBe('spawn_subagent');
  });

  test('does NOT render a duplicate block when a subagent block already exists', () => {
    openSubagentBlock('child-8', 'parent', 'task text');

    addToolResult({
      tool: 'spawn_subagent',
      params: { task: 'task text' },
      result: 'result',
      approved: true,
    });

    const toolBlocks = document.querySelectorAll('.tool-call-block');
    expect(toolBlocks.length).toBe(0);
    const subagentBlock = document.querySelector('.subagent-block[data-session-id="child-8"]');
    expect(subagentBlock).not.toBeNull();
  });
});
