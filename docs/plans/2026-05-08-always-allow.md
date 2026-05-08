# Always Allow Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add an "Always Allow" button to the tool approval modal that persists patterns to the config file and updates the in-memory broker immediately.

**Architecture:** Six layered tasks — config schema, shell utilities, broker, server, main wiring, frontend. Each task is independently testable. Shell commands use glob pattern matching with a chain/pipe safety check. Non-shell tools match by tool name only.

**Tech Stack:** Go 1.22, gorilla/websocket, vanilla JS (ES modules), app.css

---

### Task 1: Extend config — add CommandPattern field and Save function

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

---

**Step 1: Write the failing test**

Add `TestSaveAndReload` to `internal/config/config_test.go`. Append after the existing test:

```go
func TestSaveAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := &config.Config{}
	cfg.Approval.Whitelist = []config.WhitelistEntry{
		{Tool: "file_read", Allow: "always"},
		{Tool: "shell", Allow: "always", CommandPattern: "ls *"},
	}

	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(loaded.Approval.Whitelist) != 2 {
		t.Fatalf("got %d entries, want 2", len(loaded.Approval.Whitelist))
	}
	if loaded.Approval.Whitelist[1].CommandPattern != "ls *" {
		t.Errorf("got pattern %q, want %q", loaded.Approval.Whitelist[1].CommandPattern, "ls *")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
cd /Users/macminijh/projects/GoHome && go test ./internal/config/... -run TestSaveAndReload -v
```

Expected: compile error — `config.Save` undefined, `CommandPattern` undefined.

**Step 3: Add CommandPattern to WhitelistEntry and implement Save**

Replace the `WhitelistEntry` struct and add `Save` in `internal/config/config.go`:

```go
type WhitelistEntry struct {
	Tool           string `yaml:"tool"`
	Allow          string `yaml:"allow"`
	CommandPattern string `yaml:"command_pattern,omitempty"`
}
```

Add `Save` after the `Load` function:

```go
func Save(path string, cfg *Config) error {
	var err error
	path, err = expandHome(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}
```

**Step 4: Run all config tests**

```bash
cd /Users/macminijh/projects/GoHome && go test ./internal/config/... -v
```

Expected: all tests pass including `TestParseConfig` and `TestSaveAndReload`.

**Step 5: Commit**

```bash
cd /Users/macminijh/projects/GoHome && git add internal/config/config.go internal/config/config_test.go && git commit -m "feat: add CommandPattern to WhitelistEntry and config.Save"
```

---

### Task 2: Shell utilities — chain detection and glob matching

**Files:**
- Create: `internal/approval/shell.go`
- Create: `internal/approval/shell_test.go`

---

**Step 1: Write the failing tests**

Create `internal/approval/shell_test.go`:

```go
package approval

import "testing"

func TestIsChainedCommand(t *testing.T) {
	tests := []struct {
		cmd     string
		chained bool
	}{
		{"ls -la /tmp", false},
		{"ls -la /tmp && rm -rf /", true},
		{"ls | grep foo", true},
		{"ls || echo fail", true},
		{"ls; echo done", true},
		{`echo "hello && world"`, false},
		{`echo 'hello | world'`, false},
		{"", false},
	}
	for _, tc := range tests {
		got := isChainedCommand(tc.cmd)
		if got != tc.chained {
			t.Errorf("isChainedCommand(%q) = %v, want %v", tc.cmd, got, tc.chained)
		}
	}
}

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		s       string
		match   bool
	}{
		{"ls *", "ls -la", true},
		{"ls *", "ls -la /tmp", true},
		{"ls *", "cat /etc/passwd", false},
		{"git commit *", "git commit -m message", true},
		{"git commit *", "git status", false},
		{"*", "anything at all", true},
		{"exact", "exact", true},
		{"exact", "exact2", false},
		{"exact", "notexact", false},
		{"pre*suf", "preMIDDLEsuf", true},
		{"pre*suf", "presuf", true},
		{"pre*suf", "preMIDDLE", false},
	}
	for _, tc := range tests {
		got := matchGlob(tc.pattern, tc.s)
		if got != tc.match {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tc.pattern, tc.s, got, tc.match)
		}
	}
}

func TestExtractShellCommand(t *testing.T) {
	got := extractShellCommand([]byte(`{"command":"ls -la /tmp"}`))
	if got != "ls -la /tmp" {
		t.Errorf("got %q, want %q", got, "ls -la /tmp")
	}
	if extractShellCommand([]byte(`{}`)) != "" {
		t.Error("expected empty string for missing command field")
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
cd /Users/macminijh/projects/GoHome && go test ./internal/approval/... -run "TestIsChained|TestMatchGlob|TestExtractShell" -v
```

Expected: compile error — functions not defined.

**Step 3: Create shell.go**

Create `internal/approval/shell.go`:

```go
package approval

import (
	"encoding/json"
	"strings"
)

// isChainedCommand returns true if cmd contains shell chaining or piping
// operators (&&, ||, ;, |) outside of single or double quotes.
func isChainedCommand(cmd string) bool {
	inSingle, inDouble := false, false
	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]
		switch {
		case ch == '\'' && !inDouble:
			inSingle = !inSingle
		case ch == '"' && !inSingle:
			inDouble = !inDouble
		case inSingle || inDouble:
			// inside quotes — skip
		case ch == '|', ch == ';':
			return true
		case ch == '&' && i+1 < len(cmd) && cmd[i+1] == '&':
			return true
		}
	}
	return false
}

// matchGlob matches s against pattern where * matches any sequence of characters.
func matchGlob(pattern, s string) bool {
	if pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return pattern == s
	}
	idx := strings.Index(pattern, "*")
	prefix := pattern[:idx]
	rest := pattern[idx+1:]
	if !strings.HasPrefix(s, prefix) {
		return false
	}
	s = s[len(prefix):]
	if rest == "" {
		return true
	}
	for i := 0; i <= len(s); i++ {
		if matchGlob(rest, s[i:]) {
			return true
		}
	}
	return false
}

// extractShellCommand parses the "command" field from shell tool params JSON.
func extractShellCommand(params json.RawMessage) string {
	var p struct {
		Command string `json:"command"`
	}
	json.Unmarshal(params, &p) //nolint:errcheck — empty string on failure is safe
	return p.Command
}
```

**Step 4: Run tests to verify they pass**

```bash
cd /Users/macminijh/projects/GoHome && go test ./internal/approval/... -run "TestIsChained|TestMatchGlob|TestExtractShell" -v
```

Expected: all three test functions pass.

**Step 5: Commit**

```bash
cd /Users/macminijh/projects/GoHome && git add internal/approval/shell.go internal/approval/shell_test.go && git commit -m "feat: add shell chain detection and glob matching"
```

---

### Task 3: Refactor Broker — shell-aware whitelist matching and AddWhitelistEntry

**Files:**
- Modify: `internal/approval/broker.go`
- Modify: `internal/approval/broker_test.go`

---

**Step 1: Write the failing tests**

Append to `internal/approval/broker_test.go`:

```go
func TestShellAutoApproveWithPattern(t *testing.T) {
	cfg := config.ApprovalConfig{
		Whitelist: []config.WhitelistEntry{
			{Tool: "shell", Allow: "always", CommandPattern: "ls *"},
		},
	}
	broker := approval.NewBroker(cfg, nil)
	approved, err := broker.Request(context.Background(), "r10", "shell", []byte(`{"command":"ls -la /tmp"}`))
	if err != nil || !approved {
		t.Errorf("expected auto-approve; got approved=%v err=%v", approved, err)
	}
}

func TestShellPatternNoMatchFallsThrough(t *testing.T) {
	cfg := config.ApprovalConfig{
		DefaultTimeout: 1,
		Whitelist: []config.WhitelistEntry{
			{Tool: "shell", Allow: "always", CommandPattern: "ls *"},
		},
	}
	send := make(chan approval.Request, 1)
	broker := approval.NewBroker(cfg, send)
	// "cat" does not match "ls *" — should reach approval (timeout)
	_, err := broker.Request(context.Background(), "r11", "shell", []byte(`{"command":"cat /etc/passwd"}`))
	if err == nil {
		t.Error("expected timeout (approval required); got nil error")
	}
}

func TestShellChainedAlwaysPatternFallsThrough(t *testing.T) {
	cfg := config.ApprovalConfig{
		DefaultTimeout: 1,
		Whitelist: []config.WhitelistEntry{
			{Tool: "shell", Allow: "always", CommandPattern: "ls *"},
		},
	}
	send := make(chan approval.Request, 1)
	broker := approval.NewBroker(cfg, send)
	// Chained command matching the pattern must NOT be auto-approved
	_, err := broker.Request(context.Background(), "r12", "shell", []byte(`{"command":"ls /tmp && rm -rf /"}`))
	if err == nil {
		t.Error("expected timeout (chained command must reach approval); got nil error")
	}
}

func TestShellNeverWithPattern(t *testing.T) {
	cfg := config.ApprovalConfig{
		Whitelist: []config.WhitelistEntry{
			{Tool: "shell", Allow: "never", CommandPattern: "rm *"},
		},
	}
	broker := approval.NewBroker(cfg, nil)
	approved, err := broker.Request(context.Background(), "r13", "shell", []byte(`{"command":"rm -rf /tmp/foo"}`))
	if err != nil || approved {
		t.Errorf("expected auto-deny; got approved=%v err=%v", approved, err)
	}
}

func TestShellNeverChainedStillDenies(t *testing.T) {
	cfg := config.ApprovalConfig{
		Whitelist: []config.WhitelistEntry{
			{Tool: "shell", Allow: "never", CommandPattern: "rm *"},
		},
	}
	broker := approval.NewBroker(cfg, nil)
	// "never" entries apply even to chained commands
	approved, err := broker.Request(context.Background(), "r14", "shell", []byte(`{"command":"rm -rf / && echo done"}`))
	if err != nil || approved {
		t.Errorf("expected auto-deny for chained never; got approved=%v err=%v", approved, err)
	}
}

func TestAddWhitelistEntryRuntimeUpdate(t *testing.T) {
	cfg := config.ApprovalConfig{DefaultTimeout: 1}
	broker := approval.NewBroker(cfg, nil)

	// Before adding: should timeout
	_, err := broker.Request(context.Background(), "r15", "file_read", []byte(`{}`))
	if err == nil {
		t.Error("expected timeout before adding entry")
	}

	broker.AddWhitelistEntry(config.WhitelistEntry{Tool: "file_read", Allow: "always"})

	// After adding: should auto-approve
	approved, err := broker.Request(context.Background(), "r16", "file_read", []byte(`{}`))
	if err != nil || !approved {
		t.Errorf("expected auto-approve after AddWhitelistEntry; got approved=%v err=%v", approved, err)
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
cd /Users/macminijh/projects/GoHome && go test ./internal/approval/... -run "TestShell|TestAddWhitelist" -v
```

Expected: compile error — `AddWhitelistEntry` not defined.

**Step 3: Replace broker.go**

Replace the full contents of `internal/approval/broker.go`:

```go
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
	autoApproveAll bool
	defaultTimeout int
	send           chan<- Request

	mu      sync.Mutex // protects pending
	pending map[string]chan bool

	wlMu      sync.RWMutex // protects whitelist
	whitelist []config.WhitelistEntry
}

func NewBroker(cfg config.ApprovalConfig, send chan<- Request) *Broker {
	wl := make([]config.WhitelistEntry, len(cfg.Whitelist))
	copy(wl, cfg.Whitelist)
	return &Broker{
		autoApproveAll: cfg.AutoApproveAll,
		defaultTimeout: cfg.DefaultTimeout,
		send:           send,
		pending:        make(map[string]chan bool),
		whitelist:      wl,
	}
}

// AddWhitelistEntry appends an entry to the in-memory whitelist.
// Safe to call from multiple goroutines.
func (b *Broker) AddWhitelistEntry(entry config.WhitelistEntry) {
	b.wlMu.Lock()
	defer b.wlMu.Unlock()
	b.whitelist = append(b.whitelist, entry)
}

func (b *Broker) Request(ctx context.Context, id, tool string, params json.RawMessage) (bool, error) {
	b.wlMu.RLock()
	whitelist := make([]config.WhitelistEntry, len(b.whitelist))
	copy(whitelist, b.whitelist)
	b.wlMu.RUnlock()

	if tool == "shell" {
		cmd := extractShellCommand(params)
		chained := isChainedCommand(cmd)
		for _, entry := range whitelist {
			if entry.Tool != "shell" {
				continue
			}
			// No CommandPattern means match all (backward compatibility).
			matches := entry.CommandPattern == "" || matchGlob(entry.CommandPattern, cmd)
			if !matches {
				continue
			}
			switch entry.Allow {
			case "never":
				return false, nil // "never" applies even to chained commands
			case "always":
				if !chained {
					return true, nil
				}
				// chained + "always" → fall through to approval
			}
		}
	} else {
		for _, entry := range whitelist {
			if entry.Tool == tool {
				switch entry.Allow {
				case "always":
					return true, nil
				case "never":
					return false, nil
				}
			}
		}
	}

	if b.autoApproveAll {
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

	timeout := time.Duration(b.defaultTimeout) * time.Second
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

**Step 4: Run all approval tests**

```bash
cd /Users/macminijh/projects/GoHome && go test ./internal/approval/... -v
```

Expected: all tests pass — existing tests (`TestAutoApproveWhitelist`, `TestAutoDenyWhitelist`, `TestAutoApproveAll`, `TestApprovalTimeout`, `TestApprovalContextCancel`, `TestApprovalUserDecision`) plus all new ones.

**Step 5: Commit**

```bash
cd /Users/macminijh/projects/GoHome && git add internal/approval/broker.go internal/approval/broker_test.go && git commit -m "feat: shell-aware whitelist matching and AddWhitelistEntry on Broker"
```

---

### Task 4: Update Server — always_allow message handling and config persistence

**Files:**
- Modify: `internal/server/server.go`

---

**Step 1: No new test file — verify the build compiles clean after changes**

We will verify with `go build ./...` and `go test ./...` at the end of this task.

**Step 2: Replace server.go**

Replace the full contents of `internal/server/server.go`:

```go
package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
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

type Config struct {
	Store      *session.Store
	Loop       *agent.Loop
	Approval   config.ApprovalConfig
	FullConfig *config.Config // nil if no config file on disk
	ConfigPath string         // original path for saving, e.g. "~/.agent-chat/config.yaml"
}

type Server struct {
	cfg        Config
	approvalMu sync.RWMutex // protects cfg.Approval.Whitelist across connections
}

func New(cfg Config) *Server {
	return &Server{cfg: cfg}
}

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

// persistWhitelistEntry appends to the server's shared whitelist and saves to disk.
func (s *Server) persistWhitelistEntry(entry config.WhitelistEntry) {
	s.approvalMu.Lock()
	defer s.approvalMu.Unlock()
	s.cfg.Approval.Whitelist = append(s.cfg.Approval.Whitelist, entry)
	if s.cfg.FullConfig != nil {
		s.cfg.FullConfig.Approval.Whitelist = s.cfg.Approval.Whitelist
		if err := config.Save(s.cfg.ConfigPath, s.cfg.FullConfig); err != nil {
			log.Printf("always_allow: failed to save config: %v", err)
		}
	}
}

type inMsg struct {
	Type           string `json:"type"`
	SessionID      string `json:"session_id,omitempty"`
	Content        string `json:"content,omitempty"`
	RequestID      string `json:"request_id,omitempty"`
	Approved       bool   `json:"approved,omitempty"`
	Tool           string `json:"tool,omitempty"`
	CommandPattern string `json:"command_pattern,omitempty"`
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
	ping      bool
}

type wsConn struct {
	conn      *websocket.Conn
	tabID     string
	inbound   chan inMsg
	outbound  chan outMsg
	approvals chan approval.Request
	broker    *approval.Broker
	store     *session.Store
	loop      *agent.Loop
	server    *Server

	mu        sync.Mutex
	running   bool
	runCancel context.CancelFunc
	steerCh   chan string
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	tabID := r.URL.Query().Get("tab")
	if tabID == "" {
		http.Error(w, "missing tab query parameter", http.StatusBadRequest)
		return
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			return strings.HasPrefix(origin, "http://localhost") ||
				strings.HasPrefix(origin, "http://127.0.0.1") ||
				strings.HasPrefix(origin, "https://localhost") ||
				strings.HasPrefix(origin, "https://127.0.0.1")
		},
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WS upgrade: %v", err)
		return
	}

	approvalCh := make(chan approval.Request, 8)
	s.approvalMu.RLock()
	approvalCfg := s.cfg.Approval
	s.approvalMu.RUnlock()
	broker := approval.NewBroker(approvalCfg, approvalCh)

	ws := &wsConn{
		conn:      conn,
		tabID:     tabID,
		inbound:   make(chan inMsg, 16),
		outbound:  make(chan outMsg, 64),
		approvals: approvalCh,
		broker:    broker,
		store:     s.cfg.Store,
		loop:      s.cfg.Loop,
		server:    s,
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
			if msg.ping {
				if err := wc.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			} else {
				if err := wc.conn.WriteJSON(msg); err != nil {
					return
				}
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
			select {
			case wc.outbound <- outMsg{ping: true}:
			default:
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
				wc.mu.Lock()
				if wc.running {
					ch := wc.steerCh
					wc.mu.Unlock()
					if ch != nil {
						select {
						case ch <- msg.Content:
						default:
						}
					}
					continue
				}
				steerCh := make(chan string, 8)
				runCtx, cancel := context.WithCancel(ctx)
				wc.running = true
				wc.steerCh = steerCh
				wc.runCancel = cancel
				wc.mu.Unlock()
				go wc.runAgent(runCtx, msg.SessionID, msg.Content, steerCh)

			case "stop":
				wc.mu.Lock()
				if wc.runCancel != nil {
					wc.runCancel()
				}
				wc.mu.Unlock()

			case "tool_response":
				wc.broker.Respond(msg.RequestID, msg.Approved)

			case "always_allow":
				entry := config.WhitelistEntry{
					Tool:           msg.Tool,
					Allow:          "always",
					CommandPattern: msg.CommandPattern,
				}
				wc.broker.AddWhitelistEntry(entry)
				wc.server.persistWhitelistEntry(entry)
				wc.broker.Respond(msg.RequestID, true)

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

func (wc *wsConn) runAgent(ctx context.Context, sessionID, content string, steerCh chan string) {
	defer func() {
		wc.mu.Lock()
		wc.running = false
		wc.runCancel = nil
		wc.steerCh = nil
		wc.mu.Unlock()
	}()
	if wc.loop == nil {
		return
	}
	err := wc.loop.Run(ctx, sessionID, wc.tabID, content, wc.broker,
		func(token string) { wc.send(outMsg{Type: "token", Data: token}) },
		func(errMsg string) { wc.send(outMsg{Type: "error", Message: errMsg}) },
		func(tool, params, result string, approved bool) {
			wc.send(outMsg{
				Type:     "tool_result",
				Tool:     tool,
				Params:   json.RawMessage(params),
				Result:   result,
				Approved: approved,
			})
		},
		steerCh,
	)
	if err != nil || ctx.Err() != nil {
		if err != nil && ctx.Err() == nil {
			wc.send(outMsg{Type: "error", Message: err.Error()})
		} else {
			wc.send(outMsg{Type: "stopped"})
		}
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

**Step 3: Build and run all tests**

```bash
cd /Users/macminijh/projects/GoHome && go build ./... && go test ./...
```

Expected: clean build, all tests pass.

**Step 4: Commit**

```bash
cd /Users/macminijh/projects/GoHome && git add internal/server/server.go && git commit -m "feat: handle always_allow message, persist whitelist entry to config"
```

---

### Task 5: Wire FullConfig and ConfigPath in main.go

**Files:**
- Modify: `cmd/agent/main.go`

---

**Step 1: No test needed — verified by build + existing tests**

**Step 2: Update the server.New call in main.go**

Find the `srv := server.New(server.Config{` block (currently around line 108) and replace it:

```go
srv := server.New(server.Config{
    Store:      store,
    Loop:       loop,
    Approval:   cfg.Approval,
    FullConfig: cfg,
    ConfigPath: *configPath,
})
```

**Step 3: Build and run tests**

```bash
cd /Users/macminijh/projects/GoHome && go build ./... && go test ./...
```

Expected: clean build, all tests pass.

**Step 4: Commit**

```bash
cd /Users/macminijh/projects/GoHome && git add cmd/agent/main.go && git commit -m "feat: pass FullConfig and ConfigPath to server for always-allow persistence"
```

---

### Task 6: Frontend — Always Allow button and shell pattern editor

**Files:**
- Modify: `web/static/index.html`
- Modify: `web/static/app.js`
- Modify: `web/static/app.css`

---

**Step 1: Update index.html — extend the approval card**

Replace the `<div id="approval-modal" ...>` block (currently lines 30–39) with:

```html
      <div id="approval-modal" class="approval-modal" hidden>
        <div class="approval-card">
          <h3>Tool Approval Required</h3>
          <p>Tool: <code id="approval-tool"></code></p>
          <pre id="approval-params" class="params"></pre>
          <div id="always-allow-editor" hidden>
            <label class="pattern-label">Pattern</label>
            <input id="always-allow-pattern" type="text" class="pattern-input" />
            <div class="approval-buttons" style="margin-top:10px">
              <button id="always-allow-confirm" class="btn-allow">Confirm</button>
              <button id="always-allow-cancel" class="btn-deny">Cancel</button>
            </div>
          </div>
          <div id="approval-main-buttons" class="approval-buttons">
            <button id="approval-allow" class="btn-allow">Allow</button>
            <button id="approval-deny" class="btn-deny">Deny</button>
            <button id="approval-always-allow" class="btn-always-allow">Always Allow</button>
          </div>
        </div>
      </div>
```

**Step 2: Add CSS for new elements**

Append to `web/static/app.css`:

```css
.btn-always-allow {
  flex: 1; padding: 10px;
  background: #89b4fa; color: #1e1e2e;
  border: none; border-radius: 6px; cursor: pointer; font-weight: 600;
}

.pattern-label {
  display: block;
  font-size: 12px;
  font-weight: 600;
  color: #555;
  margin-bottom: 6px;
}

.pattern-input {
  width: 100%;
  padding: 8px 10px;
  border: 1px solid #ccc;
  border-radius: 6px;
  font-family: monospace;
  font-size: 13px;
  box-sizing: border-box;
  margin-bottom: 4px;
}
```

**Step 3: Update app.js**

Below the `state` declaration at the top of the file, add two helper functions:

```js
function isChainedShellCommand(cmd) {
  let inSingle = false, inDouble = false;
  for (let i = 0; i < cmd.length; i++) {
    const ch = cmd[i];
    if (ch === "'" && !inDouble) { inSingle = !inSingle; continue; }
    if (ch === '"' && !inSingle) { inDouble = !inDouble; continue; }
    if (inSingle || inDouble) continue;
    if (ch === '|' || ch === ';') return true;
    if (ch === '&' && i + 1 < cmd.length && cmd[i + 1] === '&') return true;
  }
  return false;
}

function suggestPattern(cmd) {
  const base = cmd.trim().split(/\s+/)[0] || cmd.trim();
  return base ? base + ' *' : '*';
}
```

Replace the `showApprovalModal` function:

```js
function showApprovalModal(msg) {
  const params = msg.params || {};
  const shellCmd = msg.tool === 'shell' ? (params.command || '') : '';
  const chained = msg.tool === 'shell' && isChainedShellCommand(shellCmd);

  state.awaitingApproval = { request_id: msg.request_id, tool: msg.tool, params };
  dom.approvalTool.textContent = msg.tool;
  dom.approvalParams.textContent = JSON.stringify(params, null, 2);
  dom.approvalAlwaysAllow.hidden = chained;
  dom.alwaysAllowEditor.hidden = true;
  dom.approvalMainButtons.hidden = false;
  dom.approvalModal.hidden = false;
  dom.input.disabled = true;
  dom.sendBtn.disabled = true;
}
```

Replace the `hideApprovalModal` function:

```js
function hideApprovalModal() {
  state.awaitingApproval = null;
  dom.approvalModal.hidden = true;
  dom.alwaysAllowEditor.hidden = true;
  dom.approvalMainButtons.hidden = false;
  dom.input.disabled = false;
  setBusy(state.busy);
}
```

In the `dom = { ... }` block inside `DOMContentLoaded`, add the new refs after `approvalDeny`:

```js
    approvalAlwaysAllow: document.getElementById('approval-always-allow'),
    alwaysAllowEditor:   document.getElementById('always-allow-editor'),
    alwaysAllowPattern:  document.getElementById('always-allow-pattern'),
    alwaysAllowConfirm:  document.getElementById('always-allow-confirm'),
    alwaysAllowCancel:   document.getElementById('always-allow-cancel'),
    approvalMainButtons: document.getElementById('approval-main-buttons'),
```

After the existing `dom.approvalDeny.addEventListener` block, add three new listeners:

```js
  dom.approvalAlwaysAllow.addEventListener('click', () => {
    const a = state.awaitingApproval;
    if (!a) return;
    if (a.tool === 'shell') {
      dom.alwaysAllowPattern.value = suggestPattern(a.params.command || '');
      dom.alwaysAllowEditor.hidden = false;
      dom.approvalMainButtons.hidden = true;
    } else {
      send({ type: 'always_allow', request_id: a.request_id, tool: a.tool });
      hideApprovalModal();
    }
  });

  dom.alwaysAllowConfirm.addEventListener('click', () => {
    const a = state.awaitingApproval;
    if (!a) return;
    const pattern = dom.alwaysAllowPattern.value.trim();
    if (!pattern) return;
    send({ type: 'always_allow', request_id: a.request_id, tool: a.tool, command_pattern: pattern });
    hideApprovalModal();
  });

  dom.alwaysAllowCancel.addEventListener('click', () => {
    dom.alwaysAllowEditor.hidden = true;
    dom.approvalMainButtons.hidden = false;
  });
```

**Step 4: Verify build**

```bash
cd /Users/macminijh/projects/GoHome && go build ./...
```

Expected: clean build (JS has no build step).

**Step 5: Smoke test in browser**

Start the server:

```bash
cd /Users/macminijh/projects/GoHome && go run ./cmd/agent/
```

Open `http://127.0.0.1:3000`. Send a message that triggers a tool call. Verify:
- Approval modal shows "Allow", "Deny", and "Always Allow"
- For a shell command: clicking "Always Allow" expands the pattern editor with a pre-filled suggestion
- Editing the pattern and clicking "Confirm" closes the modal and the tool executes
- The same tool triggered a second time is auto-approved without prompting
- Restarting the server and triggering the same tool again is still auto-approved (persisted to config)
- For a chained shell command (e.g. `ls && echo done`): "Always Allow" button is hidden

**Step 6: Commit**

```bash
cd /Users/macminijh/projects/GoHome && git add web/static/index.html web/static/app.js web/static/app.css && git commit -m "feat: add Always Allow button with shell pattern editor to approval modal"
```

---

## Verification

After all tasks are complete:

```bash
cd /Users/macminijh/projects/GoHome && go test ./... && go build ./...
```

All tests pass, clean build.
