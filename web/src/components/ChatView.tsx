import { h } from 'preact';
import { useRef, useEffect, useState } from 'preact/hooks';
import type { ChatMessage } from '../types';
import { ToolCallBlock } from './ToolCallBlock';

interface Props {
  messages: ChatMessage[];
  streamingContent: string;
  onSend: (content: string) => void;
  onStop: () => void;
  busy: boolean;
  disabled: boolean;
}

export function ChatView({ messages, streamingContent, onSend, onStop, busy, disabled }: Props) {
  const [input, setInput] = useState('');
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages.length, streamingContent]);

  const handleSubmit = (e: Event) => {
    e.preventDefault();
    if (!input.trim() || disabled) return;
    onSend(input.trim());
    setInput('');
  };

  return (
    <div class="chat-view">
      <div class="messages">
        {messages.map(msg => (
          <div key={msg.id} class={`message message-${msg.role}`}>
            <div class="message-role">{msg.role}</div>
            {msg.content && <div class="message-content">{msg.content}</div>}
            {msg.tool_results?.map(tr => (
              <ToolCallBlock
                key={tr.id}
                toolName={tr.tool_name}
                params={tr.params}
                result={tr.result}
                approved={tr.approved}
              />
            ))}
          </div>
        ))}
        {streamingContent && (
          <div class="message message-assistant">
            <div class="message-role">assistant</div>
            <div class="message-content">{streamingContent}</div>
          </div>
        )}
        <div ref={bottomRef} />
      </div>
      <form class="input-bar" onSubmit={handleSubmit}>
        <input
          type="text"
          value={input}
          onInput={(e) => setInput((e.target as HTMLInputElement).value)}
          placeholder={busy ? 'Agent running — type to steer...' : 'Type a message...'}
          disabled={disabled}
        />
        {busy && (
          <button type="button" class="btn-stop" onClick={onStop}>Stop</button>
        )}
        <button type="submit" disabled={disabled || !input.trim()}>Send</button>
      </form>
    </div>
  );
}
