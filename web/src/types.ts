export interface Session {
  id: string;
  title: string;
  updated_at: string;
}

export interface ToolResult {
  id: string;
  tool_name: string;
  params: string;
  result: string;
  approved: boolean;
}

export interface ChatMessage {
  id: string;
  role: 'user' | 'assistant' | 'tool';
  content: string;
  tool_calls?: unknown[];
  tool_results?: ToolResult[];
  created_at: string;
}

export type ServerMsg =
  | { type: 'token'; data: string }
  | { type: 'tool_approval'; request_id: string; tool: string; params: Record<string, unknown> }
  | { type: 'tool_result'; tool: string; params: Record<string, unknown>; result: string; approved: boolean }
  | { type: 'done'; message_id: string }
  | { type: 'error'; message: string }
  | { type: 'stopped' }
  | { type: 'sessions'; data: Session[] }
  | { type: 'history'; session_id: string; messages: ChatMessage[] };

export type ClientMsg =
  | { type: 'message'; session_id: string; content: string }
  | { type: 'tool_response'; request_id: string; approved: boolean }
  | { type: 'stop' }
  | { type: 'new_session' }
  | { type: 'load_session'; session_id: string }
  | { type: 'delete_session'; session_id: string };

export interface ApprovalRequest {
  request_id: string;
  tool: string;
  params: Record<string, unknown>;
}
