package session

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

type Session struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Message struct {
	ID          string       `json:"id"`
	SessionID   string       `json:"session_id"`
	Role        string       `json:"role"`
	Content     string       `json:"content"`
	ToolCalls   string       `json:"tool_calls,omitempty"`
	ToolCallID  string       `json:"tool_call_id,omitempty"`
	ToolResults []ToolResult `json:"tool_results,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
}

type ToolResult struct {
	ID        string    `json:"id"`
	MessageID string    `json:"message_id"`
	ToolName  string    `json:"tool_name"`
	Params    string    `json:"params"`
	Result    string    `json:"result"`
	Approved  bool      `json:"approved"`
	CreatedAt time.Time `json:"created_at"`
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("setting %s: %w", p, err)
		}
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating schema: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) CreateSession(ctx context.Context) (*Session, error) {
	id := uuid.New().String()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id) VALUES (?)`, id); err != nil {
		return nil, err
	}
	return s.getSession(ctx, id)
}

func (s *Store) getSession(ctx context.Context, id string) (*Session, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, title, created_at, updated_at FROM sessions WHERE id = ?`, id)
	var sess Session
	if err := row.Scan(&sess.ID, &sess.Title, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *Store) ListSessions(ctx context.Context) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, created_at, updated_at FROM sessions ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.ID, &sess.Title, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (s *Store) AddMessage(ctx context.Context, msg Message) (*Message, error) {
	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (id, session_id, role, content, tool_calls, tool_call_id)
		 VALUES (?, ?, ?, ?, NULLIF(?,  ''), NULLIF(?, ''))`,
		msg.ID, msg.SessionID, msg.Role, msg.Content, msg.ToolCalls, msg.ToolCallID)
	if err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE sessions SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, msg.SessionID)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

func (s *Store) GetMessages(ctx context.Context, sessionID string) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, role, content,
		        COALESCE(tool_calls,''), COALESCE(tool_call_id,''), created_at
		 FROM messages WHERE session_id = ? ORDER BY created_at ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content,
			&m.ToolCalls, &m.ToolCallID, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (s *Store) AddToolResult(ctx context.Context, tr ToolResult) (*ToolResult, error) {
	if tr.ID == "" {
		tr.ID = uuid.New().String()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tool_results (id, message_id, tool_name, params, result, approved)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		tr.ID, tr.MessageID, tr.ToolName, tr.Params, tr.Result, tr.Approved)
	if err != nil {
		return nil, err
	}
	return &tr, nil
}

func (s *Store) GetToolResults(ctx context.Context, messageID string) ([]ToolResult, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, message_id, tool_name, params, COALESCE(result,''), approved, created_at
		 FROM tool_results WHERE message_id = ? ORDER BY created_at ASC`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ToolResult
	for rows.Next() {
		var tr ToolResult
		if err := rows.Scan(&tr.ID, &tr.MessageID, &tr.ToolName, &tr.Params,
			&tr.Result, &tr.Approved, &tr.CreatedAt); err != nil {
			return nil, err
		}
		results = append(results, tr)
	}
	return results, rows.Err()
}

// GetMessagesWithResults loads messages for a session with their tool results pre-joined.
func (s *Store) GetMessagesWithResults(ctx context.Context, sessionID string) ([]Message, error) {
	msgs, err := s.GetMessages(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	for i, m := range msgs {
		if m.Role == "assistant" && m.ToolCalls != "" {
			results, err := s.GetToolResults(ctx, m.ID)
			if err != nil {
				return nil, err
			}
			msgs[i].ToolResults = results
		}
	}
	return msgs, nil
}
