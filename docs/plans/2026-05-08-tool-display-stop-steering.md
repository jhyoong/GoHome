# Tool Display, Stop, and Steering Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Show tool call inputs/outputs in the chat (collapsible), add a Stop button that cancels the running agent, and allow users to steer the agent mid-run by injecting messages before the next LLM call.

**Architecture:** Backend changes to `loop.Run` add two optional parameters — an `onToolResult` callback and a `steerCh` channel. The server creates both per-run, routes incoming messages to the channel when busy, and cancels a per-run context on "stop". Frontend renders a new `ToolCallBlock` component for tool results in history and in real time, keeps the input always enabled, and shows a Stop button while the agent is running.

**Tech Stack:** Go 1.22, gorilla/websocket, Preact + TypeScript, app.css (no CSS-in-JS)

---

### Task 1: Update loop.Run — add onToolResult callback and steerCh

**Files:**
- Modify: `internal/agent/loop.go`
- Modify: `internal/agent/loop_test.go`

---

**Step 1: Add new tests to loop_test.go**

Add `mockTool` struct and two new test functions to `internal/agent/loop_test.go`. The existing `TestSimpleMessageRoundtrip` function call must also be updated because the signature is changing.

Replace the full contents of `internal/agent/loop_test.go` with:

```go
package agent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/JiaHui/gohome/internal/agent"
	"github.com/JiaHui/gohome/internal/approval"
	"github.com/JiaHui/gohome/internal/config"
	"github.com/JiaHui/gohome/internal/llm"
	"github.com/JiaHui/gohome/internal/session"
	"github.com/JiaHui/gohome/internal/tools"
)

// mockTool is a minimal Tool implementation for tests.
type mockTool struct{ execCount int }

func (m *mockTool) Name() string                   { return "test_tool" }
func (m *mockTool) Description() string            { return "a test tool" }
func (m *mockTool) Parameters() json.RawMessage    { return json.RawMessage(`{}`) }
func (m *mockTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	m.execCount++
	return "tool output", nil
}

// sseToolCall returns SSE bytes for a single tool call response.
func sseToolCall() string {
	return strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"tc1","type":"function","function":{"name":"test_tool","arguments":"{}"}}]},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
		"",
	}, "\n\n")
}

// sseText returns SSE bytes for a simple text response.
func sseText(content string) string {
	return strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"` + content + `"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
		"",
	}, "\n\n")
}

func TestSimpleMessageRoundtrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseText("world")))
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
		nil, // onToolResult
		nil, // steerCh
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	result := strings.Join(tokens, "")
	if result != "world" {
		t.Errorf("got %q, want %q", result, "world")
	}

	msgs, _ := store.GetMessages(ctx, sess.ID)
	if len(msgs) < 1 || msgs[0].Content != "hello" {
		t.Errorf("user message not persisted: %+v", msgs)
	}
}

func TestOnToolResultCallback(t *testing.T) {
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		n := atomic.AddInt32(&callCount, 1)
		if n == 1 {
			w.Write([]byte(sseToolCall()))
		} else {
			w.Write([]byte(sseText("done")))
		}
	}))
	defer srv.Close()

	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()
	sess, _ := store.CreateSession(ctx)

	reg := tools.NewRegistry()
	mt := &mockTool{}
	reg.Register(mt)

	broker := approval.NewBroker(config.ApprovalConfig{AutoApproveAll: true}, nil)
	loop := agent.NewLoop(llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"}), reg, store, "")

	var gotTool, gotParams, gotResult string
	var gotApproved bool
	err := loop.Run(ctx, sess.ID, "tab-1", "run tool", broker,
		func(tok string) {},
		func(msg string) {},
		func(tool, params, result string, approved bool) {
			gotTool = tool
			gotParams = params
			gotResult = result
			gotApproved = approved
		},
		nil,
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotTool != "test_tool" {
		t.Errorf("onToolResult tool: got %q, want %q", gotTool, "test_tool")
	}
	if gotParams != "{}" {
		t.Errorf("onToolResult params: got %q, want %q", gotParams, "{}")
	}
	if gotResult != "tool output" {
		t.Errorf("onToolResult result: got %q, want %q", gotResult, "tool output")
	}
	if !gotApproved {
		t.Errorf("onToolResult approved: want true")
	}
	if mt.execCount != 1 {
		t.Errorf("tool executed %d times, want 1", mt.execCount)
	}
}

func TestSteeringMessageInjected(t *testing.T) {
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		n := atomic.AddInt32(&callCount, 1)
		if n == 1 {
			w.Write([]byte(sseToolCall()))
		} else {
			w.Write([]byte(sseText("ok")))
		}
	}))
	defer srv.Close()

	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()
	sess, _ := store.CreateSession(ctx)

	reg := tools.NewRegistry()
	reg.Register(&mockTool{})
	broker := approval.NewBroker(config.ApprovalConfig{AutoApproveAll: true}, nil)
	loop := agent.NewLoop(llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"}), reg, store, "")

	steerCh := make(chan string, 4)
	steerCh <- "please stop and summarize" // pre-populated; drained before second LLM call

	err := loop.Run(ctx, sess.ID, "tab-1", "hello", broker,
		func(tok string) {},
		func(msg string) {},
		nil,
		steerCh,
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	msgs, _ := store.GetMessages(ctx, sess.ID)
	var found bool
	for _, m := range msgs {
		if m.Role == "user" && m.Content == "please stop and summarize" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("steering message not saved to DB; got messages: %+v", msgs)
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
cd /Users/macminijh/projects/GoHome && go test ./internal/agent/... -v 2>&1 | head -30
```

Expected: compile error — `loop.Run` called with wrong number of arguments.

**Step 3: Update loop.Run in loop.go**

Replace the full contents of `internal/agent/loop.go` with:

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

func (l *Loop) Run(ctx context.Context, sessionID, tabID, userMessage string,
	broker *approval.Broker,
	onToken func(string),
	onError func(string),
	onToolResult func(tool, params, result string, approved bool),
	steerCh <-chan string,
) error {

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
		// Drain any steering messages before the next LLM call.
		if steerCh != nil {
		drainSteering:
			for {
				select {
				case steerMsg := <-steerCh:
					if _, err := l.store.AddMessage(ctx, session.Message{
						SessionID: sessionID, Role: "user", Content: steerMsg,
					}); err != nil {
						return fmt.Errorf("saving steering message: %w", err)
					}
					history = append(history, llm.Message{Role: "user", Content: steerMsg})
				default:
					break drainSteering
				}
			}
		}

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
				if onToolResult != nil {
					onToolResult(tc.Function.Name, tc.Function.Arguments, result, false)
				}
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
				if onToolResult != nil {
					onToolResult(tc.Function.Name, tc.Function.Arguments, result, true)
				}
			}

			toolResults = append(toolResults, llm.Message{
				Role: "tool", Content: result, ToolCallID: tc.ID, Name: tc.Function.Name,
			})
		}

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

**Step 4: Run all agent tests**

```bash
cd /Users/macminijh/projects/GoHome && go test ./internal/agent/... -v
```

Expected: all three tests pass.

**Step 5: Commit**

```bash
cd /Users/macminijh/projects/GoHome && git add internal/agent/loop.go internal/agent/loop_test.go && git commit -m "feat: add onToolResult callback and steerCh to loop.Run"
```

---

### Task 2: Update server.go — per-run cancel, steer routing, stop handler

**Files:**
- Modify: `internal/server/server.go`

---

**Step 1: Replace server.go with updated version**

Replace the full contents of `internal/server/server.go` with:

```go
package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
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
	busy      int32

	mu        sync.Mutex
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
				if !atomic.CompareAndSwapInt32(&wc.busy, 0, 1) {
					// Agent is running — route to steer channel instead of rejecting.
					wc.mu.Lock()
					ch := wc.steerCh
					wc.mu.Unlock()
					if ch != nil {
						select {
						case ch <- msg.Content:
						default:
							// Channel full; drop silently.
						}
					}
					continue
				}
				steerCh := make(chan string, 8)
				runCtx, cancel := context.WithCancel(ctx)
				wc.mu.Lock()
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
		atomic.StoreInt32(&wc.busy, 0)
		wc.mu.Lock()
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
	if err != nil {
		if ctx.Err() != nil {
			wc.send(outMsg{Type: "stopped"})
		} else {
			wc.send(outMsg{Type: "error", Message: err.Error()})
		}
		return
	}
	wc.send(outMsg{Type: "done", MessageID: ""})
	sessions, _ := wc.store.ListSessions(context.Background())
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

**Step 2: Verify build**

```bash
cd /Users/macminijh/projects/GoHome && go build ./...
```

Expected: no errors.

**Step 3: Run all tests**

```bash
cd /Users/macminijh/projects/GoHome && go test ./...
```

Expected: all pass.

**Step 4: Commit**

```bash
cd /Users/macminijh/projects/GoHome && git add internal/server/server.go && git commit -m "feat: per-run cancel context, steer routing, stop handler in server"
```

---

### Task 3: Update frontend types

**Files:**
- Modify: `web/src/types.ts`

---

**Step 1: Update types.ts**

Replace the full contents of `web/src/types.ts` with:

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
```

**Step 2: Commit**

```bash
cd /Users/macminijh/projects/GoHome && git add web/src/types.ts && git commit -m "feat: add stop/stopped/tool_result types to frontend"
```

---

### Task 4: Create ToolCallBlock component and add CSS

**Files:**
- Create: `web/src/components/ToolCallBlock.tsx`
- Modify: `web/src/app.css`

---

**Step 1: Create ToolCallBlock.tsx**

```tsx
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
```

**Step 2: Add CSS to app.css**

Append these rules to the end of `web/src/app.css`:

```css
.tool-call-block {
  margin-top: 8px;
  border: 1px solid #e0e0e0;
  border-radius: 6px;
  overflow: hidden;
  font-size: 12px;
}

.tool-call-header {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  background: #f5f5f5;
  border: none;
  cursor: pointer;
  text-align: left;
}

.tool-call-header:hover { background: #ebebeb; }

.tool-call-status.approved { color: #40a02b; font-weight: bold; }
.tool-call-status.denied { color: #d20f39; font-weight: bold; }

.tool-call-name { flex: 1; font-weight: 600; font-family: monospace; }
.tool-call-toggle { color: #666; font-size: 10px; }

.tool-call-body { padding: 8px 10px; background: #fafafa; }

.tool-call-label {
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  color: #888;
  margin-bottom: 4px;
  margin-top: 8px;
}

.tool-call-label:first-child { margin-top: 0; }

.tool-call-pre {
  background: #f0f0f0;
  padding: 6px 8px;
  border-radius: 4px;
  overflow: auto;
  max-height: 200px;
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 11px;
}

.btn-stop {
  padding: 8px 16px;
  background: #f38ba8;
  color: #1e1e2e;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  font-weight: 600;
}
```

**Step 3: Commit**

```bash
cd /Users/macminijh/projects/GoHome && git add web/src/components/ToolCallBlock.tsx web/src/app.css && git commit -m "feat: ToolCallBlock component and CSS"
```

---

### Task 5: Update ChatView

**Files:**
- Modify: `web/src/components/ChatView.tsx`

---

**Step 1: Replace ChatView.tsx**

```tsx
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
```

**Step 2: Commit**

```bash
cd /Users/macminijh/projects/GoHome && git add web/src/components/ChatView.tsx && git commit -m "feat: render tool results in ChatView, add Stop button, always-active input"
```

---

### Task 6: Update app.tsx

**Files:**
- Modify: `web/src/app.tsx`

---

**Step 1: Replace app.tsx**

```tsx
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
```

**Step 2: Verify TypeScript compiles**

```bash
cd /Users/macminijh/projects/GoHome/web && npm run build 2>&1 | tail -20
```

Expected: build succeeds with no type errors.

**Step 3: Commit**

```bash
cd /Users/macminijh/projects/GoHome && git add web/src/app.tsx && git commit -m "feat: handle tool_result, stopped events; steering input; stop button wiring"
```

---

## Verification

After all tasks are complete, run the full test suite and build:

```bash
cd /Users/macminijh/projects/GoHome && go test ./... && go build ./... && cd web && npm run build
```

All should pass with no errors.
