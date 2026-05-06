# Agent Chat Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a single-binary Go server that serves a Preact chat UI, connects to a local OpenAI-compatible LLM, executes tools with user approval, integrates MCP servers, and persists sessions in SQLite.

**Architecture:** Flat `internal/` packages for config, session, tools, approval, llm, mcp, agent, and server. The binary embeds the pre-built Preact frontend via `go:embed`. Each WebSocket connection runs 4 goroutines (wsReader, wsWriter, pingLoop, dispatcher) communicating via channels, with the agent loop running as a goroutine from the dispatcher.

**Tech Stack:** Go 1.22+, Preact + TypeScript (esbuild), `modernc.org/sqlite` (pure Go), `github.com/gorilla/websocket`, `gopkg.in/yaml.v3`, `github.com/google/uuid`.

---

### Task 1: Project Scaffold

**Files:**
- Create: `go.mod`
- Create: `Makefile`
- Create: `.gitignore`
- Create: `cmd/agent/main.go` (empty stub)
- Create: `web/package.json`
- Create: `web/tsconfig.json`
- Create: `web/src/app.tsx` (empty stub)
- Create: `web/dist/.gitkeep` (go:embed requires at least one file)
- Create: `internal/` subdirectories

**Step 1: Initialize Go module**

```bash
go mod init github.com/JiaHui/gohome
```

Expected: `go.mod` created with `module github.com/JiaHui/gohome` and `go 1.22`

**Step 2: Add Go dependencies**

```bash
go get modernc.org/sqlite
go get github.com/gorilla/websocket
go get gopkg.in/yaml.v3
go get github.com/google/uuid
```

Expected: `go.mod` and `go.sum` updated with 4 dependencies

**Step 3: Create directory structure**

```bash
mkdir -p cmd/agent internal/config internal/session internal/tools internal/approval internal/llm internal/mcp internal/agent internal/server web/src web/dist
```

**Step 4: Create `Makefile`**

```makefile
.PHONY: frontend build run clean test

frontend:
	cd web && npx esbuild src/app.tsx --bundle --outdir=dist --minify --loader:.css=css

build: frontend
	go build -o agent-chat ./cmd/agent

run: build
	./agent-chat

test:
	go test ./...

clean:
	rm -rf agent-chat web/dist web/node_modules
```

**Step 5: Create `cmd/agent/main.go`**

```go
package main

func main() {}
```

**Step 6: Create `.gitignore`**

```
agent-chat
web/dist/
web/node_modules/
*.db
```

**Step 7: Create `web/package.json`**

```json
{
  "name": "agent-chat-web",
  "version": "1.0.0",
  "dependencies": {
    "preact": "^10.19.0"
  },
  "devDependencies": {
    "esbuild": "^0.20.0",
    "typescript": "^5.3.0"
  }
}
```

**Step 8: Create `web/tsconfig.json`**

```json
{
  "compilerOptions": {
    "target": "ES2020",
    "module": "ESNext",
    "jsx": "react",
    "jsxFactory": "h",
    "jsxFragmentFactory": "Fragment",
    "strict": true,
    "moduleResolution": "bundler"
  },
  "include": ["src/**/*"]
}
```

**Step 9: Create `web/src/app.tsx` stub**

```tsx
import { h, render } from 'preact';
function App() { return <div>loading...</div>; }
render(<App />, document.getElementById('app')!);
```

**Step 10: Create `web/dist/.gitkeep`**

Empty file. Required so `go:embed web/dist` succeeds before the first frontend build.

**Step 11: Verify Go builds**

```bash
go build ./...
```

Expected: compiles without errors

**Step 12: Commit**

```bash
git add go.mod go.sum Makefile .gitignore cmd/ web/ internal/ docs/
git commit -m "feat: project scaffold - Go module, directory structure, Makefile"
```

---

### Task 2: Config Package

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

**Step 1: Write the failing test**

```go
// internal/config/config_test.go
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JiaHui/gohome/internal/config"
)

func TestParseConfig(t *testing.T) {
	yaml := `
endpoint:
  url: "http://localhost:8080/v1"
  model: "my-model"
  max_tokens: 4096
  temperature: 0.7
server:
  host: "127.0.0.1"
  port: 3000
storage:
  path: "~/.agent-chat/data.db"
approval:
  default_timeout: 300
  auto_approve_all: false
  whitelist:
    - tool: "file_read"
      allow: "always"
system_prompt: "You are helpful."
`
	f, err := os.CreateTemp("", "config*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(yaml)
	f.Close()

	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Endpoint.URL != "http://localhost:8080/v1" {
		t.Errorf("got URL %q", cfg.Endpoint.URL)
	}
	if cfg.Server.Port != 3000 {
		t.Errorf("got port %d", cfg.Server.Port)
	}
	home, _ := os.UserHomeDir()
	wantPath := filepath.Join(home, ".agent-chat/data.db")
	if cfg.Storage.Path != wantPath {
		t.Errorf("got path %q, want %q", cfg.Storage.Path, wantPath)
	}
	if len(cfg.Approval.Whitelist) != 1 || cfg.Approval.Whitelist[0].Tool != "file_read" {
		t.Errorf("unexpected whitelist: %+v", cfg.Approval.Whitelist)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/config/ -v
```

Expected: FAIL — `config` package undefined

**Step 3: Write minimal implementation**

```go
// internal/config/config.go
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type EndpointConfig struct {
	URL         string  `yaml:"url"`
	Model       string  `yaml:"model"`
	MaxTokens   int     `yaml:"max_tokens"`
	Temperature float64 `yaml:"temperature"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type StorageConfig struct {
	Path string `yaml:"path"`
}

type WhitelistEntry struct {
	Tool  string `yaml:"tool"`
	Allow string `yaml:"allow"` // "always", "never", "ask"
}

type ApprovalConfig struct {
	DefaultTimeout int              `yaml:"default_timeout"`
	AutoApproveAll bool             `yaml:"auto_approve_all"`
	Whitelist      []WhitelistEntry `yaml:"whitelist"`
}

type MCPServer struct {
	Name      string   `yaml:"name"`
	Transport string   `yaml:"transport"` // "stdio" or "sse"
	Command   string   `yaml:"command"`
	Args      []string `yaml:"args"`
	URL       string   `yaml:"url"`
}

type Config struct {
	Endpoint     EndpointConfig `yaml:"endpoint"`
	Server       ServerConfig   `yaml:"server"`
	Storage      StorageConfig  `yaml:"storage"`
	Approval     ApprovalConfig `yaml:"approval"`
	MCPServers   []MCPServer    `yaml:"mcp_servers"`
	SystemPrompt string         `yaml:"system_prompt"`
}

func Load(path string) (*Config, error) {
	var err error
	path, err = expandHome(path)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg.Storage.Path, err = expandHome(cfg.Storage.Path)
	if err != nil {
		return nil, err
	}

	if cfg.Server.Host == "" {
		cfg.Server.Host = "127.0.0.1"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 3000
	}

	return &cfg, nil
}

func expandHome(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve ~: %w", err)
	}
	return filepath.Join(home, path[1:]), nil
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/config/ -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat: config package - YAML parsing, tilde expansion, defaults"
```

---

### Task 3: Session Store

**Files:**
- Create: `internal/session/schema.sql`
- Create: `internal/session/store.go`
- Create: `internal/session/store_test.go`

**Step 1: Write the failing test**

```go
// internal/session/store_test.go
package session_test

import (
	"context"
	"testing"

	"github.com/JiaHui/gohome/internal/session"
)

func TestOpenAndMigrate(t *testing.T) {
	store, err := session.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
}

func TestSessionCRUD(t *testing.T) {
	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()

	s, err := store.CreateSession(ctx)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if s.ID == "" {
		t.Error("empty session ID")
	}

	sessions, err := store.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("want 1 session, got %d", len(sessions))
	}

	if err := store.DeleteSession(ctx, s.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	sessions, _ = store.ListSessions(ctx)
	if len(sessions) != 0 {
		t.Errorf("want 0 sessions after delete, got %d", len(sessions))
	}
}

func TestMessageCRUD(t *testing.T) {
	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()

	s, _ := store.CreateSession(ctx)
	msg, err := store.AddMessage(ctx, session.Message{
		SessionID: s.ID,
		Role:      "user",
		Content:   "hello",
	})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	if msg.ID == "" {
		t.Error("empty message ID")
	}

	msgs, err := store.GetMessages(ctx, s.ID)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Errorf("unexpected messages: %+v", msgs)
	}
}

func TestToolResultCRUD(t *testing.T) {
	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()

	s, _ := store.CreateSession(ctx)
	msg, _ := store.AddMessage(ctx, session.Message{SessionID: s.ID, Role: "assistant"})
	tr, err := store.AddToolResult(ctx, session.ToolResult{
		MessageID: msg.ID,
		ToolName:  "shell",
		Params:    `{"command":"ls"}`,
		Result:    "file.txt",
		Approved:  true,
	})
	if err != nil {
		t.Fatalf("AddToolResult: %v", err)
	}
	results, err := store.GetToolResults(ctx, msg.ID)
	if err != nil {
		t.Fatalf("GetToolResults: %v", err)
	}
	if len(results) != 1 || results[0].ID != tr.ID {
		t.Errorf("unexpected tool results: %+v", results)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/session/ -v
```

Expected: FAIL — `session` package undefined

**Step 3: Create `internal/session/schema.sql`**

```sql
CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    title       TEXT NOT NULL DEFAULT 'New Session',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS messages (
    id           TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    role         TEXT NOT NULL,
    content      TEXT,
    tool_calls   TEXT,
    tool_call_id TEXT,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tool_results (
    id         TEXT PRIMARY KEY,
    message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    tool_name  TEXT NOT NULL,
    params     TEXT NOT NULL,
    result     TEXT,
    approved   BOOLEAN NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_tool_results_message ON tool_results(message_id);
```

**Step 4: Write minimal implementation**

```go
// internal/session/store.go
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
```

**Step 5: Run test to verify it passes**

```bash
go test ./internal/session/ -v
```

Expected: PASS

**Step 6: Commit**

```bash
git add internal/session/
git commit -m "feat: session store - SQLite with WAL pragmas, schema migration, CRUD"
```

---

### Task 4: Tool Interface + Registry

**Files:**
- Create: `internal/tools/tool.go`
- Create: `internal/tools/registry.go`
- Create: `internal/tools/registry_test.go`

**Step 1: Write the failing test**

```go
// internal/tools/registry_test.go
package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/JiaHui/gohome/internal/tools"
)

type mockTool struct{ name string }

func (m *mockTool) Name() string                                                    { return m.name }
func (m *mockTool) Description() string                                             { return "mock" }
func (m *mockTool) Parameters() json.RawMessage                                     { return json.RawMessage(`{}`) }
func (m *mockTool) Execute(_ context.Context, _ json.RawMessage) (string, error) { return "ok", nil }

func TestRegistry(t *testing.T) {
	reg := tools.NewRegistry()

	if err := reg.Register(&mockTool{"foo"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := reg.Register(&mockTool{"foo"}); err == nil {
		t.Error("expected error for duplicate")
	}

	tool, ok := reg.Get("foo")
	if !ok || tool.Name() != "foo" {
		t.Error("Get failed")
	}

	reg.Deregister("foo")
	if _, ok := reg.Get("foo"); ok {
		t.Error("tool still present after Deregister")
	}

	reg.Register(&mockTool{"a"})
	reg.Register(&mockTool{"b"})
	if len(reg.All()) != 2 {
		t.Errorf("want 2 tools, got %d", len(reg.All()))
	}
}

func TestToLLMTools(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&mockTool{"mytool"})
	llmTools := reg.ToLLMTools()
	if len(llmTools) != 1 {
		t.Fatalf("want 1 tool def, got %d", len(llmTools))
	}
	fn := llmTools[0]["function"].(map[string]any)
	if fn["name"] != "mytool" {
		t.Errorf("got name %q", fn["name"])
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/tools/ -v
```

Expected: FAIL

**Step 3: Write `internal/tools/tool.go`**

```go
package tools

import (
	"context"
	"encoding/json"
)

type Tool interface {
	Name() string
	Description() string
	Parameters() json.RawMessage
	Execute(ctx context.Context, params json.RawMessage) (string, error)
}
```

**Step 4: Write `internal/tools/registry.go`**

```go
package tools

import (
	"fmt"
	"sync"
)

type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(t Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[t.Name()]; exists {
		return fmt.Errorf("tool %q already registered", t.Name())
	}
	r.tools[t.Name()] = t
	return nil
}

func (r *Registry) Deregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
}

func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) All() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

func (r *Registry) ToLLMTools() []map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]map[string]any, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name(),
				"description": t.Description(),
				"parameters":  t.Parameters(),
			},
		})
	}
	return out
}
```

**Step 5: Run test to verify it passes**

```bash
go test ./internal/tools/ -v
```

Expected: PASS

**Step 6: Commit**

```bash
git add internal/tools/tool.go internal/tools/registry.go internal/tools/registry_test.go
git commit -m "feat: tool interface and thread-safe registry"
```

---

### Task 5: Built-in Tools

**Files:**
- Create: `internal/tools/shell.go`
- Create: `internal/tools/shell_test.go`
- Create: `internal/tools/file_read.go`
- Create: `internal/tools/file_read_test.go`
- Create: `internal/tools/file_write.go`
- Create: `internal/tools/file_write_test.go`

**Step 1: Write failing tests**

```go
// internal/tools/shell_test.go
package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/JiaHui/gohome/internal/tools"
)

func TestShellTool(t *testing.T) {
	tool := &tools.ShellTool{}
	params, _ := json.Marshal(map[string]string{"command": "echo hello"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "hello\n" {
		t.Errorf("got %q, want %q", result, "hello\n")
	}
}
```

```go
// internal/tools/file_read_test.go
package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/JiaHui/gohome/internal/tools"
)

func TestFileReadTool(t *testing.T) {
	f, _ := os.CreateTemp("", "test*.txt")
	f.WriteString("hello file")
	f.Close()
	defer os.Remove(f.Name())

	tool := &tools.FileReadTool{}
	params, _ := json.Marshal(map[string]string{"path": f.Name()})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "hello file" {
		t.Errorf("got %q", result)
	}
}
```

```go
// internal/tools/file_write_test.go
package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/JiaHui/gohome/internal/tools"
)

func TestFileWriteTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "test.txt")

	tool := &tools.FileWriteTool{}
	params, _ := json.Marshal(map[string]string{"path": path, "content": "written"})
	_, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "written" {
		t.Errorf("got %q", data)
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/tools/ -run "TestShell|TestFileRead|TestFileWrite" -v
```

Expected: FAIL — `ShellTool`, `FileReadTool`, `FileWriteTool` undefined

**Step 3: Write `internal/tools/shell.go`**

```go
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
)

type ShellTool struct{}

func (s *ShellTool) Name() string { return "shell" }
func (s *ShellTool) Description() string {
	return "Execute a shell command. Returns stdout and stderr combined."
}
func (s *ShellTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"The shell command to execute"}},"required":["command"]}`)
}

func (s *ShellTool) Execute(ctx context.Context, params json.RawMessage) (string, error) {
	var p struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return "", fmt.Errorf("invalid params: %w", err)
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", p.Command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", p.Command)
	}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	_ = cmd.Run() // non-zero exit is not an error; output contains the result
	return buf.String(), nil
}
```

**Step 4: Write `internal/tools/file_read.go`**

```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

type FileReadTool struct{}

func (f *FileReadTool) Name() string        { return "file_read" }
func (f *FileReadTool) Description() string { return "Read a file and return its contents as a string." }
func (f *FileReadTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Absolute or relative path to the file"}},"required":["path"]}`)
}

func (f *FileReadTool) Execute(_ context.Context, params json.RawMessage) (string, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return "", fmt.Errorf("invalid params: %w", err)
	}
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
```

**Step 5: Write `internal/tools/file_write.go`**

```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type FileWriteTool struct{}

func (f *FileWriteTool) Name() string { return "file_write" }
func (f *FileWriteTool) Description() string {
	return "Write content to a file, creating parent directories as needed."
}
func (f *FileWriteTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
}

func (f *FileWriteTool) Execute(_ context.Context, params json.RawMessage) (string, error) {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return "", fmt.Errorf("invalid params: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(p.Path), 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(p.Path, []byte(p.Content), 0644); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(p.Content), p.Path), nil
}
```

**Step 6: Run tests to verify they pass**

```bash
go test ./internal/tools/ -v
```

Expected: all PASS

**Step 7: Commit**

```bash
git add internal/tools/
git commit -m "feat: shell, file_read, file_write built-in tools"
```

---

### Task 6: Approval Broker

**Files:**
- Create: `internal/approval/broker.go`
- Create: `internal/approval/broker_test.go`

**Step 1: Write the failing test**

```go
// internal/approval/broker_test.go
package approval_test

import (
	"context"
	"testing"
	"time"

	"github.com/JiaHui/gohome/internal/approval"
	"github.com/JiaHui/gohome/internal/config"
)

func TestAutoApproveWhitelist(t *testing.T) {
	cfg := config.ApprovalConfig{
		DefaultTimeout: 5,
		Whitelist:      []config.WhitelistEntry{{Tool: "file_read", Allow: "always"}},
	}
	broker := approval.NewBroker(cfg, nil)
	approved, err := broker.Request(context.Background(), "r1", "file_read", []byte(`{}`))
	if err != nil || !approved {
		t.Errorf("expected auto-approve; got approved=%v err=%v", approved, err)
	}
}

func TestAutoDenyWhitelist(t *testing.T) {
	cfg := config.ApprovalConfig{
		DefaultTimeout: 5,
		Whitelist:      []config.WhitelistEntry{{Tool: "shell", Allow: "never"}},
	}
	broker := approval.NewBroker(cfg, nil)
	approved, err := broker.Request(context.Background(), "r2", "shell", []byte(`{}`))
	if err != nil || approved {
		t.Errorf("expected auto-deny; got approved=%v err=%v", approved, err)
	}
}

func TestAutoApproveAll(t *testing.T) {
	cfg := config.ApprovalConfig{DefaultTimeout: 5, AutoApproveAll: true}
	broker := approval.NewBroker(cfg, nil)
	approved, err := broker.Request(context.Background(), "r3", "anything", []byte(`{}`))
	if err != nil || !approved {
		t.Errorf("expected auto-approve-all; got approved=%v err=%v", approved, err)
	}
}

func TestApprovalTimeout(t *testing.T) {
	cfg := config.ApprovalConfig{DefaultTimeout: 1} // 1 second
	send := make(chan approval.Request, 1)
	broker := approval.NewBroker(cfg, send)
	approved, err := broker.Request(context.Background(), "r4", "unknown", []byte(`{}`))
	if err == nil {
		t.Error("expected timeout error")
	}
	if approved {
		t.Error("expected false on timeout")
	}
}

func TestApprovalContextCancel(t *testing.T) {
	cfg := config.ApprovalConfig{DefaultTimeout: 60}
	send := make(chan approval.Request, 1)
	broker := approval.NewBroker(cfg, send)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	approved, err := broker.Request(ctx, "r5", "unknown", []byte(`{}`))
	if err == nil {
		t.Error("expected context error")
	}
	if approved {
		t.Error("expected false on cancel")
	}
}

func TestApprovalUserDecision(t *testing.T) {
	cfg := config.ApprovalConfig{DefaultTimeout: 5}
	send := make(chan approval.Request, 1)
	broker := approval.NewBroker(cfg, send)
	go func() {
		req := <-send
		broker.Respond(req.ID, true)
	}()
	approved, err := broker.Request(context.Background(), "r6", "unknown", []byte(`{}`))
	if err != nil || !approved {
		t.Errorf("expected user approval; got approved=%v err=%v", approved, err)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/approval/ -v
```

Expected: FAIL — `approval` package undefined

**Step 3: Write minimal implementation**

```go
// internal/approval/broker.go
package approval

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/JiaHui/gohome/internal/config"
)

var ErrApprovalTimeout = errors.New("approval timed out")

type Request struct {
	ID     string
	Tool   string
	Params json.RawMessage
}

type Broker struct {
	cfg     config.ApprovalConfig
	send    chan<- Request
	mu      sync.Mutex
	pending map[string]chan bool
}

func NewBroker(cfg config.ApprovalConfig, send chan<- Request) *Broker {
	return &Broker{
		cfg:     cfg,
		send:    send,
		pending: make(map[string]chan bool),
	}
}

func (b *Broker) Request(ctx context.Context, id, tool string, params json.RawMessage) (bool, error) {
	for _, entry := range b.cfg.Whitelist {
		if entry.Tool == tool {
			switch entry.Allow {
			case "always":
				return true, nil
			case "never":
				return false, nil
			}
		}
	}

	if b.cfg.AutoApproveAll {
		return true, nil
	}

	ch := make(chan bool, 1)
	b.mu.Lock()
	b.pending[id] = ch
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
	}()

	if b.send != nil {
		select {
		case b.send <- Request{ID: id, Tool: tool, Params: params}:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}

	timeout := time.Duration(b.cfg.DefaultTimeout) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	select {
	case decision := <-ch:
		return decision, nil
	case <-ctx.Done():
		return false, ctx.Err()
	case <-time.After(timeout):
		return false, ErrApprovalTimeout
	}
}

func (b *Broker) Respond(id string, approved bool) {
	b.mu.Lock()
	ch, ok := b.pending[id]
	b.mu.Unlock()
	if ok {
		ch <- approved
	}
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/approval/ -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/approval/
git commit -m "feat: approval broker - whitelist, auto-approve, timeout, context cancel"
```

---

### Task 7: LLM Client

**Files:**
- Create: `internal/llm/client.go`
- Create: `internal/llm/client_test.go`

**Step 1: Write failing test**

```go
// internal/llm/client_test.go
package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JiaHui/gohome/internal/config"
	"github.com/JiaHui/gohome/internal/llm"
)

func TestNonStreamingComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "hello back"}},
			},
		})
	}))
	defer srv.Close()

	client := llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"})
	resp, err := client.Complete(context.Background(), []llm.Message{{Role: "user", Content: "hello"}}, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "hello back" {
		t.Errorf("got %q", resp.Content)
	}
}

func TestStreamingTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\" there\"},\"finish_reason\":null}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"})
	var tokens []string
	var doneCalled bool
	err := client.Stream(context.Background(), []llm.Message{{Role: "user", Content: "hello"}}, nil,
		func(token string) { tokens = append(tokens, token) },
		func(_ []llm.ToolCall) {},
		func() { doneCalled = true },
	)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := ""
	for _, tk := range tokens {
		got += tk
	}
	if got != "hi there" {
		t.Errorf("got %q, want %q", got, "hi there")
	}
	if !doneCalled {
		t.Error("onDone not called")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/llm/ -v
```

Expected: FAIL

**Step 3: Write `internal/llm/client.go`**

```go
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/JiaHui/gohome/internal/config"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Response struct {
	Content   string
	ToolCalls []ToolCall
}

type Client struct {
	cfg  config.EndpointConfig
	http *http.Client
}

func NewClient(cfg config.EndpointConfig) *Client {
	return &Client{cfg: cfg, http: &http.Client{}}
}

type reqBody struct {
	Model       string        `json:"model"`
	Messages    []Message     `json:"messages"`
	Tools       []interface{} `json:"tools,omitempty"`
	Stream      bool          `json:"stream"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

func (c *Client) Complete(ctx context.Context, messages []Message, tools []interface{}) (*Response, error) {
	body := reqBody{
		Model: c.cfg.Model, Messages: messages, Tools: tools,
		Stream: false, MaxTokens: c.cfg.MaxTokens, Temperature: c.cfg.Temperature,
	}
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.URL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("LLM request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("LLM returned %d: %s", resp.StatusCode, b)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content   string     `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}
	return &Response{
		Content:   result.Choices[0].Message.Content,
		ToolCalls: result.Choices[0].Message.ToolCalls,
	}, nil
}

// Stream sends a streaming request to the LLM.
// onToken is called for each text token.
// onToolCalls is called once when finish_reason is "tool_calls", with assembled tool calls.
// onDone is called when finish_reason is "stop".
func (c *Client) Stream(ctx context.Context, messages []Message, tools []interface{},
	onToken func(string), onToolCalls func([]ToolCall), onDone func()) error {

	body := reqBody{
		Model: c.cfg.Model, Messages: messages, Tools: tools,
		Stream: true, MaxTokens: c.cfg.MaxTokens, Temperature: c.cfg.Temperature,
	}
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.URL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("LLM stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("LLM returned %d: %s", resp.StatusCode, b)
	}

	toolBuf := make(map[int]*ToolCall)

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]

		if choice.Delta.Content != "" {
			onToken(choice.Delta.Content)
		}

		for _, tc := range choice.Delta.ToolCalls {
			if _, ok := toolBuf[tc.Index]; !ok {
				toolBuf[tc.Index] = &ToolCall{ID: tc.ID, Type: tc.Type}
			}
			buf := toolBuf[tc.Index]
			buf.Function.Arguments += tc.Function.Arguments
			if tc.ID != "" {
				buf.ID = tc.ID
			}
			if tc.Function.Name != "" {
				buf.Function.Name = tc.Function.Name
			}
		}

		if choice.FinishReason != nil {
			switch *choice.FinishReason {
			case "tool_calls":
				calls := make([]ToolCall, len(toolBuf))
				for i, tc := range toolBuf {
					calls[i] = *tc
				}
				onToolCalls(calls)
			case "stop":
				if onDone != nil {
					onDone()
				}
			}
		}
	}

	return scanner.Err()
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/llm/ -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/llm/
git commit -m "feat: LLM client - non-streaming complete and streaming with tool call buffering"
```

---

### Task 8: MCP Client

**Files:**
- Create: `internal/mcp/client.go`

No automated tests — stdio spawning is environment-dependent. Verify via `go build ./...`.

**Step 1: Write `internal/mcp/client.go`**

```go
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"

	"github.com/JiaHui/gohome/internal/config"
	"github.com/JiaHui/gohome/internal/tools"
)

type Connection struct {
	name      string
	transport string
	stdin     io.WriteCloser
	stdout    *bufio.Reader
	cmd       *exec.Cmd
	sseURL    string
	nextID    int
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// MCPTool wraps a remote MCP tool as a local tools.Tool.
type MCPTool struct {
	serverName  string
	toolName    string
	description string
	parameters  json.RawMessage
	conn        *Connection
}

func (t *MCPTool) Name() string                { return t.serverName + "." + t.toolName }
func (t *MCPTool) Description() string         { return t.description }
func (t *MCPTool) Parameters() json.RawMessage { return t.parameters }

func (t *MCPTool) Execute(ctx context.Context, params json.RawMessage) (string, error) {
	return t.conn.callTool(t.toolName, params)
}

func (c *Connection) send(method string, params any) (json.RawMessage, error) {
	c.nextID++
	req := rpcRequest{JSONRPC: "2.0", ID: c.nextID, Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	switch c.transport {
	case "stdio":
		data = append(data, '\n')
		if _, err := c.stdin.Write(data); err != nil {
			return nil, fmt.Errorf("write to MCP: %w", err)
		}
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("read from MCP: %w", err)
		}
		var resp rpcResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			return nil, err
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("MCP error: %s", resp.Error.Message)
		}
		return resp.Result, nil
	case "sse":
		r, err := http.Post(c.sseURL, "application/json", bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer r.Body.Close()
		var resp rpcResponse
		if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
			return nil, err
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("MCP error: %s", resp.Error.Message)
		}
		return resp.Result, nil
	default:
		return nil, fmt.Errorf("unknown transport %q", c.transport)
	}
}

func (c *Connection) initialize() error {
	_, err := c.send("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "agent-chat", "version": "1.0"},
	})
	return err
}

func (c *Connection) listTools() ([]struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}, error) {
	result, err := c.send("tools/list", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return resp.Tools, nil
}

func (c *Connection) callTool(name string, params json.RawMessage) (string, error) {
	var p map[string]any
	if err := json.Unmarshal(params, &p); err != nil {
		return "", err
	}
	result, err := c.send("tools/call", map[string]any{"name": name, "arguments": p})
	if err != nil {
		return "", err
	}
	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return string(result), nil
	}
	var out string
	for _, item := range resp.Content {
		if item.Type == "text" {
			out += item.Text
		}
	}
	return out, nil
}

func (c *Connection) Close() {
	if c.cmd != nil {
		c.stdin.Close()
		c.cmd.Wait()
	}
}

func connectStdio(cfg config.MCPServer) (*Connection, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	conn := &Connection{
		name: cfg.Name, transport: "stdio",
		stdin: stdin, stdout: bufio.NewReader(stdout), cmd: cmd,
	}
	if err := conn.initialize(); err != nil {
		cmd.Process.Kill()
		return nil, err
	}
	return conn, nil
}

func connectSSE(cfg config.MCPServer) (*Connection, error) {
	conn := &Connection{name: cfg.Name, transport: "sse", sseURL: cfg.URL}
	if err := conn.initialize(); err != nil {
		return nil, err
	}
	return conn, nil
}

// ConnectAll connects to all configured MCP servers and registers their tools.
// Connection failures are logged and skipped — they do not block startup.
func ConnectAll(_ context.Context, servers []config.MCPServer, reg *tools.Registry) []*Connection {
	var conns []*Connection
	for _, srv := range servers {
		var (
			conn *Connection
			err  error
		)
		switch srv.Transport {
		case "stdio":
			conn, err = connectStdio(srv)
		case "sse":
			conn, err = connectSSE(srv)
		default:
			log.Printf("WARNING: MCP server %q has unknown transport %q", srv.Name, srv.Transport)
			continue
		}
		if err != nil {
			log.Printf("WARNING: MCP server %q connect failed: %v", srv.Name, err)
			continue
		}

		toolList, err := conn.listTools()
		if err != nil {
			log.Printf("WARNING: MCP server %q list tools failed: %v", srv.Name, err)
			conn.Close()
			continue
		}
		for _, t := range toolList {
			mt := &MCPTool{
				serverName: srv.Name, toolName: t.Name,
				description: t.Description, parameters: t.InputSchema, conn: conn,
			}
			if err := reg.Register(mt); err != nil {
				log.Printf("WARNING: MCP tool %q from %q skipped: %v", t.Name, srv.Name, err)
			}
		}
		conns = append(conns, conn)
	}
	return conns
}

// CloseAll closes all MCP connections.
func CloseAll(conns []*Connection) {
	for _, c := range conns {
		c.Close()
	}
}
```

**Step 2: Verify compilation**

```bash
go build ./...
```

Expected: compiles without errors

**Step 3: Commit**

```bash
git add internal/mcp/
git commit -m "feat: MCP client - stdio and SSE transport, tool discovery and wrapping"
```

---

### Task 9: Agent Loop

**Files:**
- Create: `internal/agent/loop.go`
- Create: `internal/agent/loop_test.go`

**Step 1: Write the failing test**

```go
// internal/agent/loop_test.go
package agent_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JiaHui/gohome/internal/agent"
	"github.com/JiaHui/gohome/internal/approval"
	"github.com/JiaHui/gohome/internal/config"
	"github.com/JiaHui/gohome/internal/llm"
	"github.com/JiaHui/gohome/internal/session"
	"github.com/JiaHui/gohome/internal/tools"
)

func TestSimpleMessageRoundtrip(t *testing.T) {
	// Mock LLM that streams a plain text response.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"world\"},\"finish_reason\":null}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()
	sess, _ := store.CreateSession(ctx)

	llmClient := llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"})
	reg := tools.NewRegistry()
	broker := approval.NewBroker(config.ApprovalConfig{}, nil)
	loop := agent.NewLoop(llmClient, reg, store, "")

	var tokens []string
	err := loop.Run(ctx, sess.ID, "tab-1", "hello", broker,
		func(tok string) { tokens = append(tokens, tok) },
		func(msg string) {},
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	result := ""
	for _, tk := range tokens {
		result += tk
	}
	if result != "world" {
		t.Errorf("got %q, want %q", result, "world")
	}

	// Verify user message was persisted.
	msgs, _ := store.GetMessages(ctx, sess.ID)
	if len(msgs) < 1 || msgs[0].Content != "hello" {
		t.Errorf("user message not persisted: %+v", msgs)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/agent/ -v
```

Expected: FAIL

**Step 3: Write `internal/agent/loop.go`**

```go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/JiaHui/gohome/internal/approval"
	"github.com/JiaHui/gohome/internal/llm"
	"github.com/JiaHui/gohome/internal/session"
	"github.com/JiaHui/gohome/internal/tools"
)

type Loop struct {
	llm          *llm.Client
	registry     *tools.Registry
	store        *session.Store
	systemPrompt string
}

func NewLoop(client *llm.Client, reg *tools.Registry, store *session.Store, systemPrompt string) *Loop {
	return &Loop{llm: client, registry: reg, store: store, systemPrompt: systemPrompt}
}

// Run executes the agent loop for one user message.
// broker is per-connection so callers pass it in.
// onToken is called for each streamed text token.
// onError is called for non-fatal errors to be displayed in the UI.
func (l *Loop) Run(ctx context.Context, sessionID, tabID, userMessage string,
	broker *approval.Broker, onToken func(string), onError func(string)) error {

	msgs, err := l.store.GetMessages(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("loading history: %w", err)
	}

	if _, err := l.store.AddMessage(ctx, session.Message{
		SessionID: sessionID, Role: "user", Content: userMessage,
	}); err != nil {
		return fmt.Errorf("saving user message: %w", err)
	}

	history := l.buildHistory(msgs, userMessage)
	llmTools := toAnySlice(l.registry.ToLLMTools())

	for {
		var toolCalls []llm.ToolCall
		var gotToolCalls bool

		err = l.llm.Stream(ctx, history, llmTools,
			onToken,
			func(tcs []llm.ToolCall) { toolCalls = tcs; gotToolCalls = true },
			nil,
		)
		if err != nil {
			return fmt.Errorf("LLM stream: %w", err)
		}

		if !gotToolCalls {
			break
		}

		tcJSON, _ := json.Marshal(toolCalls)
		assistantMsg, err := l.store.AddMessage(ctx, session.Message{
			SessionID: sessionID, Role: "assistant", ToolCalls: string(tcJSON),
		})
		if err != nil {
			return err
		}

		var toolResults []llm.Message
		for _, tc := range toolCalls {
			reqID := uuid.New().String()
			approved, approvalErr := broker.Request(ctx, reqID, tc.Function.Name, json.RawMessage(tc.Function.Arguments))

			var result string
			if approvalErr != nil || !approved {
				result = "denied"
				if approvalErr != nil {
					result = "error: " + approvalErr.Error()
				}
				l.store.AddToolResult(ctx, session.ToolResult{
					MessageID: assistantMsg.ID, ToolName: tc.Function.Name,
					Params: tc.Function.Arguments, Result: "", Approved: false,
				})
			} else {
				t, ok := l.registry.Get(tc.Function.Name)
				if !ok {
					result = fmt.Sprintf("tool %q not found", tc.Function.Name)
				} else {
					result, err = t.Execute(ctx, json.RawMessage(tc.Function.Arguments))
					if err != nil {
						result = "error: " + err.Error()
						log.Printf("tool %q error: %v", tc.Function.Name, err)
					}
				}
				l.store.AddToolResult(ctx, session.ToolResult{
					MessageID: assistantMsg.ID, ToolName: tc.Function.Name,
					Params: tc.Function.Arguments, Result: result, Approved: true,
				})
			}

			toolResults = append(toolResults, llm.Message{
				Role: "tool", Content: result, ToolCallID: tc.ID, Name: tc.Function.Name,
			})
		}

		// Append tool results to history and loop back to the LLM.
		history = append(history, llm.Message{Role: "assistant", ToolCalls: toolCalls})
		history = append(history, toolResults...)
	}

	return nil
}

func (l *Loop) buildHistory(msgs []session.Message, newUserMessage string) []llm.Message {
	var history []llm.Message
	if l.systemPrompt != "" {
		history = append(history, llm.Message{Role: "system", Content: l.systemPrompt})
	}
	for _, m := range msgs {
		msg := llm.Message{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		if m.ToolCalls != "" {
			json.Unmarshal([]byte(m.ToolCalls), &msg.ToolCalls)
		}
		history = append(history, msg)
	}
	history = append(history, llm.Message{Role: "user", Content: newUserMessage})
	return history
}

func toAnySlice(in []map[string]any) []interface{} {
	out := make([]interface{}, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/agent/ -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/
git commit -m "feat: agent loop - streaming, tool execution, approval integration, persistence"
```

---

### Task 10: HTTP Server + WebSocket

**Files:**
- Create: `internal/server/server.go`
- Create: `internal/server/server_test.go`

**Step 1: Write the failing test**

```go
// internal/server/server_test.go
package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JiaHui/gohome/internal/config"
	"github.com/JiaHui/gohome/internal/server"
	"github.com/JiaHui/gohome/internal/session"
)

func TestListSessions(t *testing.T) {
	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()
	store.CreateSession(ctx)
	store.CreateSession(ctx)

	srv := server.New(server.Config{Store: store, Approval: config.ApprovalConfig{}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	var sessions []map[string]any
	json.NewDecoder(resp.Body).Decode(&sessions)
	if len(sessions) != 2 {
		t.Errorf("want 2 sessions, got %d", len(sessions))
	}
}

func TestCreateSession(t *testing.T) {
	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	srv := server.New(server.Config{Store: store, Approval: config.ApprovalConfig{}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/sessions", "application/json", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("want 201, got %d", resp.StatusCode)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/server/ -v
```

Expected: FAIL

**Step 3: Write `internal/server/server.go`**

```go
package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/JiaHui/gohome/internal/agent"
	"github.com/JiaHui/gohome/internal/approval"
	"github.com/JiaHui/gohome/internal/config"
	"github.com/JiaHui/gohome/internal/session"
)

const (
	pingInterval = 30 * time.Second
	pongWait     = 40 * time.Second
	writeWait    = 10 * time.Second
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Config struct {
	Store    *session.Store
	Loop     *agent.Loop
	Approval config.ApprovalConfig
}

type Server struct {
	cfg Config
}

func New(cfg Config) *Server {
	return &Server{cfg: cfg}
}

// Handler returns the HTTP mux for the API and WebSocket endpoints.
// Static file serving is wired up separately in main.go.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)
	mux.HandleFunc("/ws", s.handleWebSocket)
	return mux
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.cfg.Store.ListSessions(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sessions == nil {
		sessions = []session.Session{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	sess, err := s.cfg.Store.CreateSession(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(sess)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.cfg.Store.DeleteSession(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- WebSocket types ---

type inMsg struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`
	Content   string `json:"content,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Approved  bool   `json:"approved,omitempty"`
}

type outMsg struct {
	Type      string          `json:"type"`
	Data      any             `json:"data,omitempty"`
	RequestID string          `json:"request_id,omitempty"`
	Tool      string          `json:"tool,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    string          `json:"result,omitempty"`
	Approved  bool            `json:"approved,omitempty"`
	Message   string          `json:"message,omitempty"`
	MessageID string          `json:"message_id,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Messages  any             `json:"messages,omitempty"`
}

// --- WebSocket connection ---

type wsConn struct {
	conn      *websocket.Conn
	tabID     string
	inbound   chan inMsg
	outbound  chan outMsg
	approvals chan approval.Request
	broker    *approval.Broker
	store     *session.Store
	loop      *agent.Loop
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	tabID := r.URL.Query().Get("tab")
	if tabID == "" {
		http.Error(w, "missing tab query parameter", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WS upgrade: %v", err)
		return
	}

	approvalCh := make(chan approval.Request, 8)
	broker := approval.NewBroker(s.cfg.Approval, approvalCh)

	ws := &wsConn{
		conn:      conn,
		tabID:     tabID,
		inbound:   make(chan inMsg, 16),
		outbound:  make(chan outMsg, 64),
		approvals: approvalCh,
		broker:    broker,
		store:     s.cfg.Store,
		loop:      s.cfg.Loop,
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go ws.reader(ctx, cancel)
	go ws.writer(ctx)
	go ws.pingLoop(ctx)
	ws.dispatcher(ctx)
}

func (wc *wsConn) reader(ctx context.Context, cancel context.CancelFunc) {
	defer cancel()
	wc.conn.SetReadDeadline(time.Now().Add(pongWait))
	wc.conn.SetPongHandler(func(string) error {
		wc.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		var msg inMsg
		if err := wc.conn.ReadJSON(&msg); err != nil {
			return
		}
		select {
		case wc.inbound <- msg:
		case <-ctx.Done():
			return
		}
	}
}

func (wc *wsConn) writer(ctx context.Context) {
	for {
		select {
		case msg := <-wc.outbound:
			wc.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := wc.conn.WriteJSON(msg); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (wc *wsConn) pingLoop(ctx context.Context) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			wc.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := wc.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (wc *wsConn) dispatcher(ctx context.Context) {
	for {
		select {
		case msg := <-wc.inbound:
			switch msg.Type {
			case "message":
				go wc.runAgent(ctx, msg.SessionID, msg.Content)
			case "tool_response":
				wc.broker.Respond(msg.RequestID, msg.Approved)
			case "new_session":
				sess, err := wc.store.CreateSession(ctx)
				if err != nil {
					wc.send(outMsg{Type: "error", Message: err.Error()})
					continue
				}
				sessions, _ := wc.store.ListSessions(ctx)
				wc.send(outMsg{Type: "sessions", Data: sessions})
				wc.send(outMsg{Type: "history", SessionID: sess.ID, Messages: []session.Message{}})
			case "load_session":
				msgs, err := wc.store.GetMessagesWithResults(ctx, msg.SessionID)
				if err != nil {
					wc.send(outMsg{Type: "error", Message: err.Error()})
					continue
				}
				if msgs == nil {
					msgs = []session.Message{}
				}
				wc.send(outMsg{Type: "history", SessionID: msg.SessionID, Messages: msgs})
			case "delete_session":
				wc.store.DeleteSession(ctx, msg.SessionID)
				sessions, _ := wc.store.ListSessions(ctx)
				wc.send(outMsg{Type: "sessions", Data: sessions})
			}
		case req := <-wc.approvals:
			wc.send(outMsg{
				Type: "tool_approval", RequestID: req.ID,
				Tool: req.Tool, Params: req.Params,
			})
		case <-ctx.Done():
			return
		}
	}
}

func (wc *wsConn) runAgent(ctx context.Context, sessionID, content string) {
	if wc.loop == nil {
		return
	}
	err := wc.loop.Run(ctx, sessionID, wc.tabID, content, wc.broker,
		func(token string) { wc.send(outMsg{Type: "token", Data: token}) },
		func(errMsg string) { wc.send(outMsg{Type: "error", Message: errMsg}) },
	)
	if err != nil && ctx.Err() == nil {
		wc.send(outMsg{Type: "error", Message: err.Error()})
		return
	}
	wc.send(outMsg{Type: "done", MessageID: ""})
	sessions, _ := wc.store.ListSessions(ctx)
	wc.send(outMsg{Type: "sessions", Data: sessions})
}

func (wc *wsConn) send(msg outMsg) {
	select {
	case wc.outbound <- msg:
	default:
		log.Printf("outbound channel full, dropping message type=%s", msg.Type)
	}
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/server/ -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/server/
git commit -m "feat: HTTP server - REST endpoints for sessions, WebSocket with 4-goroutine model"
```

---

### Task 11: Frontend — Setup, Types, App + Sidebar + ChatView

**Files:**
- Create: `web/src/types.ts`
- Modify: `web/src/app.tsx` (replace stub)
- Create: `web/src/components/Sidebar.tsx`
- Create: `web/src/components/ChatView.tsx`
- Create: `web/src/app.css`
- Create: `web/src/index.html` (entry point)

**Step 1: Install frontend dependencies**

```bash
cd web && npm install
```

Expected: `node_modules/` populated with preact, esbuild, typescript

**Step 2: Write `web/src/types.ts`**

```typescript
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
  | { type: 'tool_result'; request_id: string; tool: string; result: string; approved: boolean }
  | { type: 'done'; message_id: string }
  | { type: 'error'; message: string }
  | { type: 'sessions'; data: Session[] }
  | { type: 'history'; session_id: string; messages: ChatMessage[] };

export type ClientMsg =
  | { type: 'message'; session_id: string; content: string }
  | { type: 'tool_response'; request_id: string; approved: boolean }
  | { type: 'new_session' }
  | { type: 'load_session'; session_id: string }
  | { type: 'delete_session'; session_id: string };

export interface ApprovalRequest {
  request_id: string;
  tool: string;
  params: Record<string, unknown>;
}
```

**Step 3: Write `web/src/components/Sidebar.tsx`**

```tsx
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
```

**Step 4: Write `web/src/components/ChatView.tsx`**

```tsx
import { h } from 'preact';
import { useRef, useEffect, useState } from 'preact/hooks';
import type { ChatMessage } from '../types';

interface Props {
  messages: ChatMessage[];
  streamingContent: string;
  onSend: (content: string) => void;
  disabled: boolean;
}

export function ChatView({ messages, streamingContent, onSend, disabled }: Props) {
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
            <div class="message-content">{msg.content}</div>
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
          placeholder="Type a message..."
          disabled={disabled}
        />
        <button type="submit" disabled={disabled || !input.trim()}>Send</button>
      </form>
    </div>
  );
}
```

**Step 5: Write `web/src/app.tsx`**

```tsx
import { h, render } from 'preact';
import { useState, useEffect, useRef, useCallback } from 'preact/hooks';
import { Sidebar } from './components/Sidebar';
import { ChatView } from './components/ChatView';
import type { Session, ChatMessage, ServerMsg, ClientMsg, ApprovalRequest } from './types';
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
          break;
        case 'tool_approval':
          setAwaitingApproval({ request_id: msg.request_id, tool: msg.tool, params: msg.params });
          break;
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
        case 'error':
          setError(msg.message);
          setBusy(false);
          break;
      }
    };

    ws.onopen = () => {
      // Reload session list on connect/reconnect.
      ws.send(JSON.stringify({ type: 'new_session' } satisfies ClientMsg));
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
    setBusy(true);
    send({ type: 'message', session_id: activeSessionId, content });
  };

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
          disabled={busy || awaitingApproval !== null}
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
```

**Step 6: Write `web/src/app.css`**

```css
*, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }

body {
  font-family: system-ui, -apple-system, sans-serif;
  font-size: 14px;
  background: #f5f5f5;
  color: #1a1a1a;
  height: 100vh;
  overflow: hidden;
}

.layout {
  display: flex;
  height: 100vh;
}

.sidebar {
  width: 240px;
  background: #1e1e2e;
  color: #cdd6f4;
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
}

.sidebar-header {
  padding: 12px;
  border-bottom: 1px solid #313244;
}

.btn-new {
  width: 100%;
  padding: 8px;
  background: #89b4fa;
  color: #1e1e2e;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  font-weight: 600;
}

.session-list {
  list-style: none;
  overflow-y: auto;
  flex: 1;
}

.session-list li {
  display: flex;
  align-items: center;
  padding: 8px 12px;
  cursor: pointer;
  border-bottom: 1px solid #313244;
}

.session-list li:hover, .session-list li.active {
  background: #313244;
}

.session-title {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.btn-delete {
  background: none;
  border: none;
  color: #f38ba8;
  cursor: pointer;
  font-size: 16px;
  padding: 0 4px;
  opacity: 0;
}

.session-list li:hover .btn-delete { opacity: 1; }

.main {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  position: relative;
}

.error-banner {
  background: #f38ba8;
  color: #1e1e2e;
  padding: 8px 16px;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.chat-view {
  display: flex;
  flex-direction: column;
  flex: 1;
  overflow: hidden;
}

.messages {
  flex: 1;
  overflow-y: auto;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.message {
  max-width: 80%;
  padding: 10px 14px;
  border-radius: 8px;
}

.message-user {
  align-self: flex-end;
  background: #89b4fa;
  color: #1e1e2e;
}

.message-assistant {
  align-self: flex-start;
  background: #fff;
  border: 1px solid #e0e0e0;
}

.message-role {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  opacity: 0.6;
  margin-bottom: 4px;
}

.message-content {
  white-space: pre-wrap;
  word-break: break-word;
}

.input-bar {
  display: flex;
  gap: 8px;
  padding: 12px 16px;
  border-top: 1px solid #e0e0e0;
  background: #fff;
}

.input-bar input {
  flex: 1;
  padding: 8px 12px;
  border: 1px solid #ccc;
  border-radius: 6px;
  font-size: 14px;
  outline: none;
}

.input-bar input:focus { border-color: #89b4fa; }

.input-bar button {
  padding: 8px 16px;
  background: #89b4fa;
  color: #1e1e2e;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  font-weight: 600;
}

.input-bar button:disabled { opacity: 0.5; cursor: default; }

.approval-modal {
  position: absolute;
  inset: 0;
  background: rgba(0,0,0,0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 10;
}

.approval-card {
  background: #fff;
  border-radius: 10px;
  padding: 24px;
  max-width: 500px;
  width: 90%;
  box-shadow: 0 8px 32px rgba(0,0,0,0.2);
}

.approval-card h3 { margin-bottom: 12px; }
.approval-card p { margin-bottom: 8px; }
.approval-card code { background: #f0f0f0; padding: 2px 6px; border-radius: 4px; }

.params {
  background: #f5f5f5;
  padding: 12px;
  border-radius: 6px;
  overflow: auto;
  max-height: 200px;
  margin-bottom: 16px;
  font-size: 12px;
}

.approval-buttons { display: flex; gap: 10px; }

.btn-allow {
  flex: 1; padding: 10px;
  background: #a6e3a1; color: #1e1e2e;
  border: none; border-radius: 6px; cursor: pointer; font-weight: 600;
}

.btn-deny {
  flex: 1; padding: 10px;
  background: #f38ba8; color: #1e1e2e;
  border: none; border-radius: 6px; cursor: pointer; font-weight: 600;
}
```

**Step 7: Write `web/src/index.html`**

(This file is referenced by esbuild as the HTML entry point)

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>Agent Chat</title>
</head>
<body>
  <div id="app"></div>
  <script src="app.js"></script>
</body>
</html>
```

**Step 8: Update Makefile to include CSS and copy index.html**

Modify the `frontend` target in `Makefile`:

```makefile
frontend:
	cd web && npx esbuild src/app.tsx --bundle --outdir=dist --minify \
		--loader:.css=css --jsx-factory=h --jsx-fragment=Fragment
	cp web/src/index.html web/dist/index.html
```

**Step 9: Build the frontend to verify it compiles**

```bash
make frontend
```

Expected: `web/dist/app.js` and `web/dist/app.css` created without errors

**Step 10: Commit**

```bash
git add web/
git commit -m "feat: Preact frontend - App, Sidebar, ChatView, ApprovalModal, CSS"
```

---

### Task 12: Main Entry Point + Static Embed + Graceful Shutdown

**Files:**
- Modify: `cmd/agent/main.go`

**Step 1: Write the full main.go**

```go
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/JiaHui/gohome/internal/agent"
	"github.com/JiaHui/gohome/internal/config"
	"github.com/JiaHui/gohome/internal/llm"
	"github.com/JiaHui/gohome/internal/mcp"
	"github.com/JiaHui/gohome/internal/server"
	"github.com/JiaHui/gohome/internal/session"
	"github.com/JiaHui/gohome/internal/tools"
)

//go:embed ../../web/dist
var webDist embed.FS

var version = "dev"

func main() {
	var (
		configPath = flag.String("config", "~/.agent-chat/config.yaml", "Path to config file")
		port       = flag.Int("port", 0, "Override server port")
		host       = flag.String("host", "", "Override server host")
		dbPath     = flag.String("db", "", "Override database path")
		verbose    = flag.Bool("verbose", false, "Enable debug logging")
		showVer    = flag.Bool("version", false, "Print version and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("agent-chat", version)
		os.Exit(0)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		// If config file not found, use defaults.
		if os.IsNotExist(err) {
			log.Printf("INFO: config file not found at %s, using defaults", *configPath)
			cfg = &config.Config{}
			cfg.Server.Host = "127.0.0.1"
			cfg.Server.Port = 3000
		} else {
			log.Fatalf("loading config: %v", err)
		}
	}

	if *port != 0 {
		cfg.Server.Port = *port
	}
	if *host != "" {
		cfg.Server.Host = *host
	}
	if *dbPath != "" {
		cfg.Storage.Path = *dbPath
	}
	if cfg.Storage.Path == "" {
		home, _ := os.UserHomeDir()
		cfg.Storage.Path = filepath.Join(home, ".agent-chat", "data.db")
	}
	if cfg.Endpoint.URL == "" {
		cfg.Endpoint.URL = "http://localhost:8080/v1"
	}

	if *verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
		log.Printf("DEBUG: config loaded: endpoint=%s model=%s", cfg.Endpoint.URL, cfg.Endpoint.Model)
	}

	if cfg.Server.Host == "0.0.0.0" || cfg.Server.Host == "::" {
		log.Println("WARNING: Server is listening on all interfaces with no authentication.")
		log.Println("WARNING: Any device on your network can access this agent and execute tools.")
	}

	if err := os.MkdirAll(filepath.Dir(cfg.Storage.Path), 0755); err != nil {
		log.Fatalf("creating storage dir: %v", err)
	}
	store, err := session.Open(cfg.Storage.Path)
	if err != nil {
		log.Fatalf("opening database: %v", err)
	}
	defer store.Close()

	reg := tools.NewRegistry()
	reg.Register(&tools.ShellTool{})
	reg.Register(&tools.FileReadTool{})
	reg.Register(&tools.FileWriteTool{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mcpConns := mcp.ConnectAll(ctx, cfg.MCPServers, reg)
	defer mcp.CloseAll(mcpConns)

	llmClient := llm.NewClient(cfg.Endpoint)
	loop := agent.NewLoop(llmClient, reg, store, cfg.SystemPrompt)

	srv := server.New(server.Config{
		Store:    store,
		Loop:     loop,
		Approval: cfg.Approval,
	})

	staticFS, err := fs.Sub(webDist, "web/dist")
	if err != nil {
		log.Fatalf("embed sub: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", srv.Handler())
	mux.HandleFunc("/ws", srv.Handler().ServeHTTP)
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	httpSrv := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler: mux,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("shutting down...")
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		httpSrv.Shutdown(shutdownCtx)
	}()

	ln, err := net.Listen("tcp", httpSrv.Addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	log.Printf("agent-chat listening on http://%s", httpSrv.Addr)
	if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Printf("server error: %v", err)
	}
}
```

Note: The `mux` above needs adjustment — `/api/` and `/ws` should route through the server handler, and `/` falls back to static files. Replace the mux setup with:

```go
apiHandler := srv.Handler()
mux := http.NewServeMux()
mux.Handle("/api/", apiHandler)
mux.Handle("/ws", apiHandler)
mux.Handle("/", http.FileServer(http.FS(staticFS)))
```

**Step 2: Verify the full project compiles**

```bash
go build ./...
```

Expected: compiles without errors

**Step 3: Run all tests**

```bash
go test ./...
```

Expected: all packages PASS

**Step 4: Build the full binary**

```bash
make build
```

Expected: `agent-chat` binary created

**Step 5: Smoke test**

Create a minimal config file:

```bash
mkdir -p ~/.agent-chat
cat > /tmp/test-config.yaml << 'EOF'
endpoint:
  url: "http://localhost:8080/v1"
  model: "test"
server:
  host: "127.0.0.1"
  port: 3000
EOF
./agent-chat --config /tmp/test-config.yaml &
sleep 1
curl -s http://localhost:3000/api/sessions
kill %1
```

Expected: `[]` returned from sessions endpoint, no crash

**Step 6: Commit**

```bash
git add cmd/agent/main.go
git commit -m "feat: main entry point - embed static files, wire all components, graceful shutdown"
```

---

## Final Verification

Run the complete test suite and build one last time:

```bash
go test ./... && make build
```

Expected:
- All `go test` output shows PASS
- `agent-chat` binary is created

### Manual End-to-End Smoke Test

1. Start a local LLM endpoint (llama.cpp or OMLX) on `http://localhost:8080/v1`
2. Create `~/.agent-chat/config.yaml` using the full example from the spec
3. Run `./agent-chat`
4. Open `http://localhost:3000` in a browser
5. Verify: session list loads, new chat opens, message sends and LLM response streams