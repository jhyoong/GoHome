import { h } from 'preact';
import { useState } from 'preact/hooks';

interface Props {
  toolName: string;
  params: string;
  result: string;
  approved: boolean;
}

export function ToolCallBlock({ toolName, params, result, approved }: Props) {
  const [expanded, setExpanded] = useState(false);

  return (
    <div class="tool-call-block">
      <button class="tool-call-header" onClick={() => setExpanded(e => !e)}>
        <span class={`tool-call-status ${approved ? 'approved' : 'denied'}`}>
          {approved ? '✓' : '✗'}
        </span>
        <span class="tool-call-name">{toolName}</span>
        <span class="tool-call-toggle">{expanded ? '▲' : '▼'}</span>
      </button>
      {expanded && (
        <div class="tool-call-body">
          <div class="tool-call-label">Input</div>
          <pre class="tool-call-pre">{formatJSON(params)}</pre>
          <div class="tool-call-label">Output</div>
          <pre class="tool-call-pre">{result || '(empty)'}</pre>
        </div>
      )}
    </div>
  );
}

function formatJSON(s: string): string {
  try {
    return JSON.stringify(JSON.parse(s), null, 2);
  } catch {
    return s;
  }
}
