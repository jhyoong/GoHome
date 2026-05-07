import { h, render } from 'preact';
import { useState, useEffect, useRef, useCallback } from 'preact/hooks';
import { Sidebar } from './components/Sidebar';
import { ChatView } from './components/ChatView';
import type { Session, ChatMessage, ServerMsg, ClientMsg, ApprovalRequest, ToolResult } from './types';
import './app.css';

function App() {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [activeSessionId, setActiveSessionId] = useState<string | null>(null);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [streamingContent, setStreamingContent] = useState('');
  const [awaitingApproval, setAwaitingApproval] = useState<ApprovalRequest | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const tabID = useRef(crypto.randomUUID());
  const activeSessionIdRef = useRef<string | null>(null);

  const send = useCallback((msg: ClientMsg) => {
    wsRef.current?.send(JSON.stringify(msg));
  }, []);

  const connect = useCallback(() => {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${proto}//${location.host}/ws?tab=${tabID.current}`);

    ws.onmessage = (e) => {
      const msg = JSON.parse(e.data) as ServerMsg;
      switch (msg.type) {
        case 'token':
          setStreamingContent(prev => prev + msg.data);
          break;
        case 'sessions':
          setSessions(msg.data);
          break;
        case 'history':
          setMessages(msg.messages);
          setStreamingContent('');
          setActiveSessionId(msg.session_id);
          activeSessionIdRef.current = msg.session_id;
          break;
        case 'tool_approval':
          setAwaitingApproval({ request_id: msg.request_id, tool: msg.tool, params: msg.params });
          break;
        case 'tool_result': {
          const tr: ToolResult = {
            id: crypto.randomUUID(),
            tool_name: msg.tool,
            params: JSON.stringify(msg.params),
            result: msg.result,
            approved: msg.approved,
          };
          const syntheticMsg: ChatMessage = {
            id: crypto.randomUUID(),
            role: 'assistant',
            content: '',
            tool_results: [tr],
            created_at: new Date().toISOString(),
          };
          setMessages(m => [...m, syntheticMsg]);
          break;
        }
        case 'done':
          setStreamingContent(prev => {
            if (prev) {
              const fakeMsg: ChatMessage = {
                id: msg.message_id || crypto.randomUUID(),
                role: 'assistant',
                content: prev,
                created_at: new Date().toISOString(),
              };
              setMessages(m => [...m, fakeMsg]);
            }
            return '';
          });
          setBusy(false);
          break;
        case 'stopped':
          setStreamingContent('');
          setBusy(false);
          break;
        case 'error':
          setError(msg.message);
          setBusy(false);
          break;
      }
    };

    ws.onopen = () => {
      const sid = activeSessionIdRef.current;
      if (sid) {
        ws.send(JSON.stringify({ type: 'load_session', session_id: sid } satisfies ClientMsg));
      } else {
        ws.send(JSON.stringify({ type: 'new_session' } satisfies ClientMsg));
      }
    };

    let retryDelay = 1000;
    ws.onclose = () => {
      wsRef.current = null;
      setTimeout(() => {
        retryDelay = Math.min(retryDelay * 2, 30000);
        connect();
      }, retryDelay);
    };

    wsRef.current = ws;
  }, []);

  useEffect(() => {
    connect();
    return () => wsRef.current?.close();
  }, []);

  const handleSend = (content: string) => {
    if (!activeSessionId) return;
    const userMsg: ChatMessage = {
      id: crypto.randomUUID(), role: 'user', content,
      created_at: new Date().toISOString(),
    };
    setMessages(m => [...m, userMsg]);
    if (!busy) setBusy(true);
    send({ type: 'message', session_id: activeSessionId, content });
  };

  const handleStop = useCallback(() => {
    send({ type: 'stop' });
  }, [send]);

  const handleApproval = (approved: boolean) => {
    if (!awaitingApproval) return;
    send({ type: 'tool_response', request_id: awaitingApproval.request_id, approved });
    setAwaitingApproval(null);
  };

  return (
    <div class="layout">
      <Sidebar
        sessions={sessions}
        activeSessionId={activeSessionId}
        onSelect={(id) => send({ type: 'load_session', session_id: id })}
        onNew={() => send({ type: 'new_session' })}
        onDelete={(id) => send({ type: 'delete_session', session_id: id })}
      />
      <main class="main">
        {error && <div class="error-banner">{error} <button onClick={() => setError(null)}>×</button></div>}
        <ChatView
          messages={messages}
          streamingContent={streamingContent}
          onSend={handleSend}
          onStop={handleStop}
          busy={busy}
          disabled={awaitingApproval !== null}
        />
        {awaitingApproval && (
          <div class="approval-modal">
            <div class="approval-card">
              <h3>Tool Approval Required</h3>
              <p>Tool: <code>{awaitingApproval.tool}</code></p>
              <pre class="params">{JSON.stringify(awaitingApproval.params, null, 2)}</pre>
              <div class="approval-buttons">
                <button onClick={() => handleApproval(true)} class="btn-allow">Allow</button>
                <button onClick={() => handleApproval(false)} class="btn-deny">Deny</button>
              </div>
            </div>
          </div>
        )}
      </main>
    </div>
  );
}

render(<App />, document.getElementById('app')!);
