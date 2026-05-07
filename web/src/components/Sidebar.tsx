import { h } from 'preact';
import type { Session } from '../types';

interface Props {
  sessions: Session[];
  activeSessionId: string | null;
  onSelect: (id: string) => void;
  onNew: () => void;
  onDelete: (id: string) => void;
}

export function Sidebar({ sessions, activeSessionId, onSelect, onNew, onDelete }: Props) {
  return (
    <div class="sidebar">
      <div class="sidebar-header">
        <button onClick={onNew} class="btn-new">New Chat</button>
      </div>
      <ul class="session-list">
        {sessions.map(s => (
          <li key={s.id} class={s.id === activeSessionId ? 'active' : ''}>
            <span onClick={() => onSelect(s.id)} class="session-title">{s.title}</span>
            <button onClick={(e) => { e.stopPropagation(); onDelete(s.id); }} class="btn-delete">×</button>
          </li>
        ))}
      </ul>
    </div>
  );
}
