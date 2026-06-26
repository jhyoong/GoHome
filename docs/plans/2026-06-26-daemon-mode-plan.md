# Daemon Mode Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the single-process model with a daemon-only architecture where the agent runs as a background process communicating with the TUI over JSON-RPC 2.0 on a Unix socket.

**Architecture:** The binary detects whether a daemon is running (via `~/.gohome/daemon.sock`). If not, it forks a daemon and connects. The daemon owns agent, tools, guard, sessions, and LLM clients. The TUI is a thin Bubble Tea client that receives `agent.event` notifications and sends requests (`session.input`, `session.approval`, etc.) over JSON-RPC. Subagents are goroutines in the daemon, each with their own `RPCFrontend` that tags events with the child's `sessionID`.

**Tech Stack:** Go 1.25, `net` (Unix sockets), `encoding/json` (JSON-RPC 2.0 codec), Bubble Tea (TUI), existing `agent.Frontend` interface.

**Design doc:** `docs/plans/2026-06-26-daemon-mode-design.md`

---

### Task 1: JSON-RPC 2.0 Codec

Build the low-level JSON-RPC 2.0 message types and codec. This is the foundation everything else builds on.

**Files:**
- Create: `gohome/internal/daemon/rpc/message.go`
- Test: `gohome/internal/daemon/rpc/message_test.go`

**Step 1: Write failing tests for JSON-RPC message encoding/decoding**

```go
package rpc

import (
	"encoding/json"
	"testing"
)

func TestEncodeRequest(t *testing.T) {
	req := Request{
		ID:     NewID(1),
		Method: "daemon.health",
		Params: json.RawMessage(`{}`),
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if string(m["jsonrpc"]) != `"2.0"` {
		t.Errorf("jsonrpc: got %s", m["jsonrpc"])
	}
	if string(m["method"]) != `"daemon.health"` {
		t.Errorf("method: got %s", m["method"])
	}
}

func TestEncodeNotification(t *testing.T) {
	n := Request{
		Method: "agent.event",
		Params: json.RawMessage(`{"sessionID":"s1"}`),
	}
	data, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, hasID := m["id"]; hasID {
		t.Error("notification should not have id field")
	}
}

func TestEncodeResponse(t *testing.T) {
	resp := Response{
		ID:     NewID(1),
		Result: json.RawMessage(`{"version":"dev"}`),
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if string(m["jsonrpc"]) != `"2.0"` {
		t.Errorf("jsonrpc: got %s", m["jsonrpc"])
	}
}

func TestEncodeErrorResponse(t *testing.T) {
	resp := Response{
		ID:    NewID(1),
		Error: &Error{Code: -32600, Message: "invalid request"},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, hasResult := m["result"]; hasResult {
		t.Error("error response should not have result field")
	}
}

func TestDecodeMessage_Request(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"method":"session.input","params":{"text":"hello"}}`
	msg, err := Decode([]byte(raw))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !msg.IsRequest() {
		t.Error("expected IsRequest")
	}
	if msg.Method != "session.input" {
		t.Errorf("method: got %q", msg.Method)
	}
}

func TestDecodeMessage_Notification(t *testing.T) {
	raw := `{"jsonrpc":"2.0","method":"agent.event","params":{}}`
	msg, err := Decode([]byte(raw))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !msg.IsNotification() {
		t.Error("expected IsNotification")
	}
}

func TestDecodeMessage_Response(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`
	msg, err := Decode([]byte(raw))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !msg.IsResponse() {
		t.Error("expected IsResponse")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/daemon/rpc/ -v`
Expected: FAIL (package does not exist)

**Step 3: Implement the JSON-RPC message types**

```go
package rpc

import "encoding/json"

// ID represents a JSON-RPC request ID (integer or string).
type ID struct {
	num    int64
	str    string
	isStr  bool
	isNull bool
}

func NewID(n int64) *ID               { return &ID{num: n} }
func NewStringID(s string) *ID        { return &ID{str: s, isStr: true} }

func (id *ID) MarshalJSON() ([]byte, error) {
	if id == nil || id.isNull {
		return []byte("null"), nil
	}
	if id.isStr {
		return json.Marshal(id.str)
	}
	return json.Marshal(id.num)
}

func (id *ID) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		id.isNull = true
		return nil
	}
	if err := json.Unmarshal(data, &id.num); err == nil {
		return nil
	}
	id.isStr = true
	return json.Unmarshal(data, &id.str)
}

// Request is a JSON-RPC 2.0 request or notification.
// If ID is nil, it is a notification.
type Request struct {
	ID     *ID              `json:"id,omitempty"`
	Method string           `json:"method"`
	Params json.RawMessage  `json:"params,omitempty"`
}

func (r Request) MarshalJSON() ([]byte, error) {
	m := map[string]any{"jsonrpc": "2.0", "method": r.Method}
	if r.ID != nil {
		m["id"] = r.ID
	}
	if r.Params != nil {
		m["params"] = r.Params
	}
	return json.Marshal(m)
}

// Error is a JSON-RPC 2.0 error object.
type Error struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Data    json.RawMessage  `json:"data,omitempty"`
}

func (e *Error) Error() string { return e.Message }

// Response is a JSON-RPC 2.0 response.
type Response struct {
	ID     *ID              `json:"id"`
	Result json.RawMessage  `json:"result,omitempty"`
	Error  *Error           `json:"error,omitempty"`
}

func (r Response) MarshalJSON() ([]byte, error) {
	m := map[string]any{"jsonrpc": "2.0", "id": r.ID}
	if r.Error != nil {
		m["error"] = r.Error
	} else {
		m["result"] = r.Result
	}
	return json.Marshal(m)
}

// Message is a decoded JSON-RPC 2.0 message (request, notification, or response).
type Message struct {
	ID     *ID
	Method string
	Params json.RawMessage
	Result json.RawMessage
	Error  *Error
}

func (m *Message) IsRequest()      bool { return m.Method != "" && m.ID != nil }
func (m *Message) IsNotification() bool { return m.Method != "" && m.ID == nil }
func (m *Message) IsResponse()     bool { return m.Method == "" }

// Decode parses a raw JSON-RPC 2.0 message.
func Decode(data []byte) (*Message, error) {
	var raw struct {
		JSONRPC string           `json:"jsonrpc"`
		ID      *ID              `json:"id,omitempty"`
		Method  string           `json:"method,omitempty"`
		Params  json.RawMessage  `json:"params,omitempty"`
		Result  json.RawMessage  `json:"result,omitempty"`
		Error   *Error           `json:"error,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return &Message{
		ID:     raw.ID,
		Method: raw.Method,
		Params: raw.Params,
		Result: raw.Result,
		Error:  raw.Error,
	}, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/daemon/rpc/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/daemon/rpc/message.go gohome/internal/daemon/rpc/message_test.go
git commit -m "feat(daemon): add JSON-RPC 2.0 message types and codec"
```

---

### Task 2: JSON-RPC Transport (Conn)

Build a `Conn` type that reads/writes JSON-RPC messages over any `net.Conn`, using newline-delimited JSON framing.

**Files:**
- Create: `gohome/internal/daemon/rpc/conn.go`
- Test: `gohome/internal/daemon/rpc/conn_test.go`

**Step 1: Write failing tests for Conn read/write**

```go
package rpc

import (
	"encoding/json"
	"net"
	"testing"
)

func TestConn_WriteAndRead(t *testing.T) {
	c1, c2 := net.Pipe()
	conn1 := NewConn(c1)
	conn2 := NewConn(c2)
	defer conn1.Close()
	defer conn2.Close()

	req := Request{
		ID:     NewID(1),
		Method: "daemon.health",
		Params: json.RawMessage(`{}`),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		msg, err := conn2.Read()
		if err != nil {
			t.Errorf("Read: %v", err)
			return
		}
		if !msg.IsRequest() {
			t.Errorf("expected request, got response/notification")
		}
		if msg.Method != "daemon.health" {
			t.Errorf("method: got %q", msg.Method)
		}
	}()

	if err := conn1.WriteRequest(req); err != nil {
		t.Fatalf("WriteRequest: %v", err)
	}
	<-done
}

func TestConn_WriteNotification(t *testing.T) {
	c1, c2 := net.Pipe()
	conn1 := NewConn(c1)
	conn2 := NewConn(c2)
	defer conn1.Close()
	defer conn2.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		msg, err := conn2.Read()
		if err != nil {
			t.Errorf("Read: %v", err)
			return
		}
		if !msg.IsNotification() {
			t.Errorf("expected notification")
		}
	}()

	if err := conn1.Notify("agent.event", json.RawMessage(`{"sessionID":"s1"}`)); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	<-done
}

func TestConn_WriteResponse(t *testing.T) {
	c1, c2 := net.Pipe()
	conn1 := NewConn(c1)
	conn2 := NewConn(c2)
	defer conn1.Close()
	defer conn2.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		msg, err := conn2.Read()
		if err != nil {
			t.Errorf("Read: %v", err)
			return
		}
		if !msg.IsResponse() {
			t.Errorf("expected response")
		}
	}()

	resp := Response{
		ID:     NewID(1),
		Result: json.RawMessage(`{"ok":true}`),
	}
	if err := conn1.WriteResponse(resp); err != nil {
		t.Fatalf("WriteResponse: %v", err)
	}
	<-done
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/daemon/rpc/ -run TestConn -v`
Expected: FAIL (Conn not defined)

**Step 3: Implement Conn**

```go
package rpc

import (
	"bufio"
	"encoding/json"
	"net"
	"sync"
)

// Conn wraps a net.Conn with JSON-RPC message framing (newline-delimited JSON).
type Conn struct {
	conn    net.Conn
	scanner *bufio.Scanner
	mu      sync.Mutex // serializes writes
}

// NewConn wraps a net.Conn for JSON-RPC communication.
func NewConn(c net.Conn) *Conn {
	return &Conn{
		conn:    c,
		scanner: bufio.NewScanner(c),
	}
}

// Read blocks until a complete JSON-RPC message arrives.
func (c *Conn) Read() (*Message, error) {
	if !c.scanner.Scan() {
		if err := c.scanner.Err(); err != nil {
			return nil, err
		}
		return nil, net.ErrClosed
	}
	return Decode(c.scanner.Bytes())
}

// WriteRequest sends a JSON-RPC request.
func (c *Conn) WriteRequest(req Request) error {
	return c.writeJSON(req)
}

// WriteResponse sends a JSON-RPC response.
func (c *Conn) WriteResponse(resp Response) error {
	return c.writeJSON(resp)
}

// Notify sends a JSON-RPC notification (request without ID).
func (c *Conn) Notify(method string, params json.RawMessage) error {
	return c.writeJSON(Request{Method: method, Params: params})
}

// Close closes the underlying connection.
func (c *Conn) Close() error {
	return c.conn.Close()
}

func (c *Conn) writeJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	data = append(data, '\n')
	_, err = c.conn.Write(data)
	return err
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/daemon/rpc/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/daemon/rpc/conn.go gohome/internal/daemon/rpc/conn_test.go
git commit -m "feat(daemon): add JSON-RPC Conn transport over net.Conn"
```

---

### Task 3: JSON-RPC Pending Calls (Request/Response Correlation)

Add a `PendingCalls` helper that tracks in-flight requests and correlates responses by ID. Both daemon and TUI need this for the bidirectional request/response pattern (e.g., `approval.request` from daemon to TUI).

**Files:**
- Create: `gohome/internal/daemon/rpc/pending.go`
- Test: `gohome/internal/daemon/rpc/pending_test.go`

**Step 1: Write failing tests**

```go
package rpc

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestPending_CallAndResolve(t *testing.T) {
	p := NewPending()
	ctx := context.Background()

	done := make(chan struct{})
	var result json.RawMessage

	go func() {
		defer close(done)
		var err error
		result, err = p.Call(ctx, 1)
		if err != nil {
			t.Errorf("Call: %v", err)
		}
	}()

	// Give the goroutine time to register the call.
	time.Sleep(10 * time.Millisecond)

	p.Resolve(1, json.RawMessage(`{"ok":true}`), nil)
	<-done

	if string(result) != `{"ok":true}` {
		t.Errorf("result: got %s", result)
	}
}

func TestPending_CallCancelled(t *testing.T) {
	p := NewPending()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.Call(ctx, 2)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestPending_ResolveWithError(t *testing.T) {
	p := NewPending()
	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := p.Call(ctx, 3)
		if err == nil {
			t.Error("expected error from RPC error response")
		}
	}()

	time.Sleep(10 * time.Millisecond)
	p.Resolve(3, nil, &Error{Code: -32600, Message: "bad request"})
	<-done
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/daemon/rpc/ -run TestPending -v`
Expected: FAIL

**Step 3: Implement PendingCalls**

```go
package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

type pendingResult struct {
	data json.RawMessage
	err  *Error
}

// Pending tracks in-flight JSON-RPC requests awaiting responses.
type Pending struct {
	mu    sync.Mutex
	calls map[int64]chan pendingResult
}

func NewPending() *Pending {
	return &Pending{calls: make(map[int64]chan pendingResult)}
}

// Call registers an in-flight request with the given ID and blocks until
// Resolve is called with a matching ID or ctx is cancelled.
func (p *Pending) Call(ctx context.Context, id int64) (json.RawMessage, error) {
	ch := make(chan pendingResult, 1)
	p.mu.Lock()
	p.calls[id] = ch
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		delete(p.calls, id)
		p.mu.Unlock()
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			return nil, fmt.Errorf("rpc error %d: %s", res.err.Code, res.err.Message)
		}
		return res.data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Resolve delivers a response to a waiting Call. If no call is pending for
// the given ID, the response is silently dropped.
func (p *Pending) Resolve(id int64, result json.RawMessage, rpcErr *Error) {
	p.mu.Lock()
	ch, ok := p.calls[id]
	p.mu.Unlock()
	if ok {
		ch <- pendingResult{data: result, err: rpcErr}
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/daemon/rpc/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/daemon/rpc/pending.go gohome/internal/daemon/rpc/pending_test.go
git commit -m "feat(daemon): add PendingCalls for request/response correlation"
```

---

### Task 4: Protocol Types

Define the JSON-serializable param/result structs for every RPC method in the protocol. These are shared between daemon and TUI.

**Files:**
- Create: `gohome/internal/daemon/rpc/protocol.go`
- Test: `gohome/internal/daemon/rpc/protocol_test.go`

**Step 1: Write failing test for protocol type round-trip**

```go
package rpc

import (
	"encoding/json"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
)

func TestAgentEventParams_RoundTrip(t *testing.T) {
	ev := agent.Event{
		Kind:      agent.EventTokenDelta,
		SessionID: "s1",
		TextDelta: "hello",
	}
	p := AgentEventParams{SessionID: "s1", Event: ev}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var p2 AgentEventParams
	if err := json.Unmarshal(data, &p2); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if p2.SessionID != "s1" {
		t.Errorf("sessionID: got %q", p2.SessionID)
	}
	if p2.Event.Kind != agent.EventTokenDelta {
		t.Errorf("event kind: got %v", p2.Event.Kind)
	}
}

func TestSessionInputParams_RoundTrip(t *testing.T) {
	p := SessionInputParams{SessionID: "s1", Text: "hello world"}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var p2 SessionInputParams
	if err := json.Unmarshal(data, &p2); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if p2.Text != "hello world" {
		t.Errorf("text: got %q", p2.Text)
	}
}

func TestHealthResult_RoundTrip(t *testing.T) {
	r := HealthResult{Version: "dev", UptimeSeconds: 42}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var r2 HealthResult
	if err := json.Unmarshal(data, &r2); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if r2.Version != "dev" {
		t.Errorf("version: got %q", r2.Version)
	}
	if r2.UptimeSeconds != 42 {
		t.Errorf("uptime: got %d", r2.UptimeSeconds)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/daemon/rpc/ -run "TestAgentEvent|TestSessionInput|TestHealth" -v`
Expected: FAIL

**Step 3: Implement protocol types**

```go
package rpc

import (
	"encoding/json"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/session"
	"github.com/jhyoong/GoHome/gohome/internal/tui"
)

// Method constants.
const (
	MethodAgentEvent     = "agent.event"
	MethodSessionState   = "session.state"
	MethodSessionInput   = "session.input"
	MethodSessionApproval = "session.approval"
	MethodSessionNew     = "session.new"
	MethodSessionResume  = "session.resume"
	MethodSessionList    = "session.list"
	MethodSessionCancel  = "session.cancel"
	MethodModelSet       = "model.set"
	MethodDaemonHealth   = "daemon.health"
	MethodDaemonStop     = "daemon.stop"
	MethodApprovalRequest = "approval.request"
)

// --- Daemon -> TUI Notifications ---

type AgentEventParams struct {
	SessionID string      `json:"sessionID"`
	Event     agent.Event `json:"event"`
}

type SessionStateParams struct {
	SessionID       string              `json:"sessionID"`
	Model           string              `json:"model"`
	Yolo            bool                `json:"yolo"`
	Timeline        []tui.TimelineEntry `json:"timeline"`
	PendingApproval *ApprovalRequestParams `json:"pendingApproval,omitempty"`
}

// --- TUI -> Daemon Requests ---

type SessionInputParams struct {
	SessionID string `json:"sessionID"`
	Text      string `json:"text"`
}

type SessionApprovalParams struct {
	SessionID string                 `json:"sessionID"`
	Decision  guard.ApprovalDecision `json:"decision"`
}

type SessionResumeParams struct {
	ID string `json:"id"`
}

type ModelSetParams struct {
	Name string `json:"name"`
}

type ModelSetResult struct {
	ModelName     string `json:"modelName"`
	ContextWindow int    `json:"contextWindow"`
}

type SessionNewResult struct {
	SessionID string `json:"sessionID"`
}

type SessionResumeResult struct {
	SessionID string `json:"sessionID"`
	// History is sent as raw JSON to avoid import cycles in callers
	// that don't need to inspect individual messages.
	History json.RawMessage `json:"history"`
}

type SessionListResult struct {
	Sessions []session.Listing `json:"sessions"`
}

type SessionCancelParams struct {
	SessionID string `json:"sessionID"`
}

type HealthResult struct {
	Version       string `json:"version"`
	UptimeSeconds int64  `json:"uptimeSeconds"`
}

// --- Daemon -> TUI Requests ---

type ApprovalRequestParams struct {
	SessionID        string          `json:"sessionID"`
	Tool             string          `json:"tool"`
	Input            json.RawMessage `json:"input"`
	Summary          string          `json:"summary"`
	SuggestedPattern string          `json:"suggestedPattern"`
}

type ApprovalResponseResult struct {
	Decision guard.ApprovalDecision `json:"decision"`
}
```

Note: The `agent.Event` struct must be JSON-serializable. Check that all fields have JSON tags. If `agent.Event` does not have JSON tags, add them in a preparatory step (see Task 5).

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/daemon/rpc/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/daemon/rpc/protocol.go gohome/internal/daemon/rpc/protocol_test.go
git commit -m "feat(daemon): add JSON-RPC protocol types for all RPC methods"
```

---

### Task 5: Add JSON Tags to agent.Event

`agent.Event` (`gohome/internal/agent/events.go:36-49`) needs JSON tags for serialization over JSON-RPC. The `EventKind` type is already a string, which serializes cleanly.

Similarly, `agent.ToolResult` (`events.go:30-34`) needs tags.

**Files:**
- Modify: `gohome/internal/agent/events.go:30-49`
- Test: `gohome/internal/agent/events_test.go` (add JSON round-trip test)

**Step 1: Write failing test for Event JSON round-trip**

Add to `gohome/internal/agent/events_test.go`:

```go
func TestEvent_JSONRoundTrip(t *testing.T) {
	ev := Event{
		Kind:      EventTokenDelta,
		SessionID: "s1",
		TextDelta: "hello",
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var ev2 Event
	if err := json.Unmarshal(data, &ev2); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if ev2.Kind != EventTokenDelta {
		t.Errorf("Kind: got %v", ev2.Kind)
	}
	if ev2.TextDelta != "hello" {
		t.Errorf("TextDelta: got %q", ev2.TextDelta)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/agent/ -run TestEvent_JSONRoundTrip -v`
Expected: FAIL or incorrect field mapping (fields without JSON tags use Go field names which differ from desired wire names)

**Step 3: Add JSON tags**

In `gohome/internal/agent/events.go`, update the structs:

```go
type ToolResult struct {
	ToolUseID string `json:"toolUseID"`
	Content   string `json:"content"`
	IsError   bool   `json:"isError"`
}

type Event struct {
	Kind          EventKind    `json:"kind"`
	SessionID     string       `json:"sessionID"`
	TextDelta     string       `json:"textDelta,omitempty"`
	ToolCallID    string       `json:"toolCallID,omitempty"`
	ToolName      string       `json:"toolName,omitempty"`
	InputJSON     string       `json:"inputJSON,omitempty"`
	Result        *ToolResult  `json:"result,omitempty"`
	Usage         *common.Usage `json:"usage,omitempty"`
	StopReason    string       `json:"stopReason,omitempty"`
	EndReason     string       `json:"endReason,omitempty"`
	Err           error        `json:"-"`
	ThinkingDelta string       `json:"thinkingDelta,omitempty"`
}
```

Note: `Err error` gets `json:"-"` because errors don't serialize cleanly. For the wire protocol, error events carry the error message in a separate field. Add an `ErrMessage` field:

```go
	Err           error        `json:"-"`
	ErrMessage    string       `json:"errMessage,omitempty"`
```

Populate `ErrMessage` when creating `EventError` events. This affects `turn.go:104-110` where `EventError` is emitted -- set `ErrMessage: ev.Err.Error()`.

**Step 4: Run all agent tests to verify nothing breaks**

Run: `go test ./gohome/internal/agent/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/agent/events.go gohome/internal/agent/events_test.go gohome/internal/agent/turn.go
git commit -m "feat(agent): add JSON tags to Event and ToolResult for wire serialization"
```

---

### Task 6: RPCFrontend (Daemon-Side Frontend)

This is the key piece: an `agent.Frontend` implementation that serializes all three methods over a JSON-RPC `Conn`. The daemon creates one `RPCFrontend` per agent (parent gets one, each child gets one tagged with its `sessionID`).

**Files:**
- Create: `gohome/internal/daemon/frontend/frontend.go`
- Test: `gohome/internal/daemon/frontend/frontend_test.go`

**Step 1: Write failing tests**

```go
package frontend

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
)

func TestRPCFrontend_Emit(t *testing.T) {
	c1, c2 := net.Pipe()
	conn1 := rpc.NewConn(c1)
	conn2 := rpc.NewConn(c2)
	defer conn1.Close()
	defer conn2.Close()

	fe := New(conn1)

	done := make(chan struct{})
	go func() {
		defer close(done)
		msg, err := conn2.Read()
		if err != nil {
			t.Errorf("Read: %v", err)
			return
		}
		if !msg.IsNotification() {
			t.Error("expected notification")
		}
		if msg.Method != rpc.MethodAgentEvent {
			t.Errorf("method: got %q", msg.Method)
		}
	}()

	fe.Emit("s1", agent.Event{Kind: agent.EventTokenDelta, TextDelta: "hi"})
	<-done
}

func TestRPCFrontend_RequestApproval(t *testing.T) {
	c1, c2 := net.Pipe()
	conn1 := rpc.NewConn(c1)
	conn2 := rpc.NewConn(c2)
	defer conn1.Close()
	defer conn2.Close()

	fe := New(conn1)

	// Simulate TUI responding to approval request.
	go func() {
		msg, err := conn2.Read()
		if err != nil {
			t.Errorf("Read: %v", err)
			return
		}
		if msg.Method != rpc.MethodApprovalRequest {
			t.Errorf("method: got %q", msg.Method)
		}
		result, _ := json.Marshal(rpc.ApprovalResponseResult{
			Decision: guard.ApprovalDecision{Outcome: guard.AllowOnce},
		})
		conn2.WriteResponse(rpc.Response{
			ID:     msg.ID,
			Result: result,
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	dec, err := fe.RequestApproval(ctx, guard.ApprovalRequest{
		SessionID: "s1",
		Tool:      "bash",
		Input:     json.RawMessage(`{"command":"ls"}`),
	})
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if dec.Outcome != guard.AllowOnce {
		t.Errorf("outcome: got %v", dec.Outcome)
	}
}

func TestRPCFrontend_AwaitUserInput(t *testing.T) {
	c1, c2 := net.Pipe()
	conn1 := rpc.NewConn(c1)
	conn2 := rpc.NewConn(c2)
	defer conn1.Close()
	defer conn2.Close()

	fe := New(conn1)

	// Simulate TUI sending session.input.
	go func() {
		time.Sleep(10 * time.Millisecond)
		params, _ := json.Marshal(rpc.SessionInputParams{SessionID: "s1", Text: "hello"})
		conn2.WriteRequest(rpc.Request{
			ID:     rpc.NewID(1),
			Method: rpc.MethodSessionInput,
			Params: params,
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	text, err := fe.AwaitUserInput(ctx, "s1")
	if err != nil {
		t.Fatalf("AwaitUserInput: %v", err)
	}
	if text != "hello" {
		t.Errorf("text: got %q", text)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/daemon/frontend/ -v`
Expected: FAIL

**Step 3: Implement RPCFrontend**

```go
package frontend

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
)

var _ agent.Frontend = (*RPCFrontend)(nil)

// RPCFrontend implements agent.Frontend by serializing events and requests
// over a JSON-RPC Conn. One instance per agent (parent or child).
type RPCFrontend struct {
	conn    *rpc.Conn
	pending *rpc.Pending
	idSeq   atomic.Int64
	inputCh chan string
}

// New creates an RPCFrontend that writes to conn.
func New(conn *rpc.Conn) *RPCFrontend {
	return &RPCFrontend{
		conn:    conn,
		pending: rpc.NewPending(),
		inputCh: make(chan string, 1),
	}
}

// Emit sends an agent event as a JSON-RPC notification.
func (f *RPCFrontend) Emit(sessionID string, ev agent.Event) {
	params := rpc.AgentEventParams{SessionID: sessionID, Event: ev}
	data, err := json.Marshal(params)
	if err != nil {
		return
	}
	_ = f.conn.Notify(rpc.MethodAgentEvent, data)
}

// RequestApproval sends an approval.request RPC to the TUI and blocks until
// the TUI responds or ctx is cancelled.
func (f *RPCFrontend) RequestApproval(ctx context.Context, req guard.ApprovalRequest) (guard.ApprovalDecision, error) {
	id := f.idSeq.Add(1)
	params := rpc.ApprovalRequestParams{
		SessionID:        req.SessionID,
		Tool:             req.Tool,
		Input:            req.Input,
		Summary:          req.Summary,
		SuggestedPattern: req.SuggestedPattern,
	}
	data, err := json.Marshal(params)
	if err != nil {
		return guard.ApprovalDecision{Outcome: guard.Deny}, err
	}
	if err := f.conn.WriteRequest(rpc.Request{
		ID:     rpc.NewID(id),
		Method: rpc.MethodApprovalRequest,
		Params: data,
	}); err != nil {
		return guard.ApprovalDecision{Outcome: guard.Deny}, err
	}

	result, err := f.pending.Call(ctx, id)
	if err != nil {
		return guard.ApprovalDecision{Outcome: guard.Deny}, err
	}

	var resp rpc.ApprovalResponseResult
	if err := json.Unmarshal(result, &resp); err != nil {
		return guard.ApprovalDecision{Outcome: guard.Deny}, fmt.Errorf("unmarshal approval response: %w", err)
	}
	return resp.Decision, nil
}

// AwaitUserInput blocks until the daemon dispatches a session.input message
// for this frontend's session, or ctx is cancelled.
func (f *RPCFrontend) AwaitUserInput(ctx context.Context, _ string) (string, error) {
	select {
	case text := <-f.inputCh:
		return text, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// DeliverInput pushes user input text into the input channel.
// Called by the daemon when it receives a session.input request.
func (f *RPCFrontend) DeliverInput(text string) {
	f.inputCh <- text
}

// ResolvePending delivers a response to a pending RPC call (e.g., approval).
// Called by the daemon's read loop when a response arrives.
func (f *RPCFrontend) ResolvePending(id int64, result json.RawMessage, rpcErr *rpc.Error) {
	f.pending.Resolve(id, result, rpcErr)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/daemon/frontend/ -v`
Expected: PASS

Note: The `AwaitUserInput` test requires that the daemon's read loop calls `DeliverInput`. The test above simulates this by having the TUI side send a `session.input` request. The test for `AwaitUserInput` will need adjustment -- the daemon read loop (built in Task 7) handles dispatching `session.input` to `DeliverInput`. For now, test `AwaitUserInput` directly:

```go
func TestRPCFrontend_AwaitUserInput(t *testing.T) {
	c1, _ := net.Pipe()
	conn1 := rpc.NewConn(c1)
	defer conn1.Close()

	fe := New(conn1)

	go func() {
		time.Sleep(10 * time.Millisecond)
		fe.DeliverInput("hello")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	text, err := fe.AwaitUserInput(ctx, "s1")
	if err != nil {
		t.Fatalf("AwaitUserInput: %v", err)
	}
	if text != "hello" {
		t.Errorf("text: got %q", text)
	}
}
```

**Step 5: Commit**

```bash
git add gohome/internal/daemon/frontend/frontend.go gohome/internal/daemon/frontend/frontend_test.go
git commit -m "feat(daemon): add RPCFrontend implementing agent.Frontend over JSON-RPC"
```

---

### Task 7: Daemon Server

Build the daemon server that listens on a Unix socket, accepts one client, owns the agent lifecycle, and dispatches incoming JSON-RPC requests.

**Files:**
- Create: `gohome/internal/daemon/server.go`
- Test: `gohome/internal/daemon/server_test.go`

**Step 1: Write failing tests**

```go
package daemon

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
)

func TestServer_HealthCheck(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")

	srv, err := NewServer(sock, ServerConfig{Version: "test"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Stop()

	go srv.Serve()

	// Give server time to start listening.
	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	c := rpc.NewConn(conn)
	params, _ := json.Marshal(struct{}{})
	if err := c.WriteRequest(rpc.Request{
		ID:     rpc.NewID(1),
		Method: rpc.MethodDaemonHealth,
		Params: params,
	}); err != nil {
		t.Fatalf("WriteRequest: %v", err)
	}

	msg, err := c.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !msg.IsResponse() {
		t.Fatal("expected response")
	}
	var result rpc.HealthResult
	if err := json.Unmarshal(msg.Result, &result); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if result.Version != "test" {
		t.Errorf("version: got %q", result.Version)
	}
}

func TestServer_Stop(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")

	srv, err := NewServer(sock, ServerConfig{Version: "test"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	done := make(chan struct{})
	go func() {
		srv.Serve()
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	// Connect and send daemon.stop.
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	c := rpc.NewConn(conn)
	params, _ := json.Marshal(struct{}{})
	c.WriteRequest(rpc.Request{
		ID:     rpc.NewID(1),
		Method: rpc.MethodDaemonStop,
		Params: params,
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop within timeout")
	}
	conn.Close()
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/daemon/ -v`
Expected: FAIL

**Step 3: Implement Server**

```go
package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/daemon/frontend"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
)

// ServerConfig holds the daemon server configuration.
type ServerConfig struct {
	Version      string
	GracePeriod  time.Duration // how long to wait after TUI disconnect before exiting
}

// Server is the daemon process that owns the agent and communicates with
// the TUI over JSON-RPC on a Unix socket.
type Server struct {
	listener  net.Listener
	sockPath  string
	config    ServerConfig
	startedAt time.Time
	cancel    context.CancelFunc
	ctx       context.Context
	wg        sync.WaitGroup

	// mu guards client and fe.
	mu     sync.Mutex
	client *rpc.Conn
	fe     *frontend.RPCFrontend
}

// NewServer creates a daemon server listening on the given Unix socket path.
func NewServer(sockPath string, cfg ServerConfig) (*Server, error) {
	if cfg.GracePeriod == 0 {
		cfg.GracePeriod = 30 * time.Second
	}
	_ = os.Remove(sockPath) // clean stale socket

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		listener:  ln,
		sockPath:  sockPath,
		config:    cfg,
		startedAt: time.Now(),
		ctx:       ctx,
		cancel:    cancel,
	}, nil
}

// Serve accepts connections and handles them. Blocks until Stop is called
// or a daemon.stop RPC is received.
func (s *Server) Serve() {
	defer s.cleanup()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.ctx.Err() != nil {
				return
			}
			slog.Error("daemon: accept error", "err", err)
			continue
		}
		s.handleClient(conn)
		if s.ctx.Err() != nil {
			return
		}
	}
}

// Stop signals the server to shut down.
func (s *Server) Stop() {
	s.cancel()
	_ = s.listener.Close()
}

func (s *Server) cleanup() {
	_ = os.Remove(s.sockPath)
}

func (s *Server) handleClient(conn net.Conn) {
	c := rpc.NewConn(conn)
	fe := frontend.New(c)

	s.mu.Lock()
	s.client = c
	s.fe = fe
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.client = nil
		s.fe = nil
		s.mu.Unlock()
		_ = c.Close()
	}()

	for {
		msg, err := c.Read()
		if err != nil {
			if s.ctx.Err() != nil {
				return
			}
			slog.Debug("daemon: client read error", "err", err)
			return
		}
		s.dispatch(c, msg)
		if s.ctx.Err() != nil {
			return
		}
	}
}

func (s *Server) dispatch(c *rpc.Conn, msg *rpc.Message) {
	if msg.IsResponse() {
		s.mu.Lock()
		fe := s.fe
		s.mu.Unlock()
		if fe != nil && msg.ID != nil {
			fe.ResolvePending(msg.ID.Int64(), msg.Result, msg.Error)
		}
		return
	}

	switch msg.Method {
	case rpc.MethodDaemonHealth:
		s.handleHealth(c, msg)
	case rpc.MethodDaemonStop:
		s.handleStop(c, msg)
	case rpc.MethodSessionInput:
		s.handleSessionInput(msg)
	case rpc.MethodSessionApproval:
		// Will be implemented when wiring the full agent lifecycle.
	case rpc.MethodSessionNew:
		// Will be implemented when wiring the full agent lifecycle.
	case rpc.MethodSessionResume:
		// Will be implemented when wiring the full agent lifecycle.
	case rpc.MethodSessionList:
		// Will be implemented when wiring the full agent lifecycle.
	case rpc.MethodSessionCancel:
		// Will be implemented when wiring the full agent lifecycle.
	case rpc.MethodModelSet:
		// Will be implemented when wiring the full agent lifecycle.
	default:
		if msg.ID != nil {
			c.WriteResponse(rpc.Response{
				ID:    msg.ID,
				Error: &rpc.Error{Code: -32601, Message: "method not found: " + msg.Method},
			})
		}
	}
}

func (s *Server) handleHealth(c *rpc.Conn, msg *rpc.Message) {
	result, _ := json.Marshal(rpc.HealthResult{
		Version:       s.config.Version,
		UptimeSeconds: int64(time.Since(s.startedAt).Seconds()),
	})
	if msg.ID != nil {
		c.WriteResponse(rpc.Response{ID: msg.ID, Result: result})
	}
}

func (s *Server) handleStop(c *rpc.Conn, msg *rpc.Message) {
	if msg.ID != nil {
		result, _ := json.Marshal(struct{}{})
		c.WriteResponse(rpc.Response{ID: msg.ID, Result: result})
	}
	s.Stop()
}

func (s *Server) handleSessionInput(msg *rpc.Message) {
	var params rpc.SessionInputParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return
	}
	s.mu.Lock()
	fe := s.fe
	s.mu.Unlock()
	if fe != nil {
		fe.DeliverInput(params.Text)
	}
}
```

Note: `msg.ID.Int64()` requires adding an `Int64()` method to the `rpc.ID` type. Add this in `message.go`:

```go
func (id *ID) Int64() int64 {
	if id == nil {
		return 0
	}
	return id.num
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/daemon/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/daemon/server.go gohome/internal/daemon/server_test.go gohome/internal/daemon/rpc/message.go
git commit -m "feat(daemon): add Server with Unix socket listener and JSON-RPC dispatch"
```

---

### Task 8: Wire Agent to Daemon Server

Connect the agent lifecycle to the daemon server. The server creates the agent, tools, guard, and session state on startup, and runs the agent loop when input arrives.

**Files:**
- Modify: `gohome/internal/daemon/server.go`
- Test: `gohome/internal/daemon/server_test.go` (add integration test)

**Step 1: Write failing integration test**

Add to `server_test.go`:

```go
func TestServer_AgentRoundTrip(t *testing.T) {
	// This test verifies: client sends session.input, daemon runs the agent,
	// client receives agent.event notifications.
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")

	// Use a fakeClient that returns a simple text response.
	turn := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: "hello from agent"},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	llmClient := &fakeClient{sequences: [][]common.StreamEvent{turn}}

	srv, err := NewServer(sock, ServerConfig{
		Version:   "test",
		LLMClient: llmClient,
		// ... other config needed to build the agent
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Stop()

	go srv.Serve()
	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	c := rpc.NewConn(conn)

	// Send user input.
	params, _ := json.Marshal(rpc.SessionInputParams{SessionID: "main", Text: "hi"})
	c.WriteRequest(rpc.Request{
		ID:     rpc.NewID(1),
		Method: rpc.MethodSessionInput,
		Params: params,
	})

	// Read agent event notifications.
	var gotTokenDelta bool
	deadline := time.After(2 * time.Second)
	for !gotTokenDelta {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for token delta")
		default:
		}
		msg, err := c.Read()
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if msg.IsNotification() && msg.Method == rpc.MethodAgentEvent {
			var ep rpc.AgentEventParams
			json.Unmarshal(msg.Params, &ep)
			if ep.Event.Kind == agent.EventTokenDelta {
				gotTokenDelta = true
				if ep.Event.TextDelta != "hello from agent" {
					t.Errorf("text delta: got %q", ep.Event.TextDelta)
				}
			}
		}
	}
}
```

This test will require extending `ServerConfig` with fields for LLM client, settings, etc. The exact shape will depend on what the server needs to construct the agent. The general pattern: `ServerConfig` accepts pre-built dependencies (LLM client, guard, registry, settings) so tests can inject fakes.

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/daemon/ -run TestServer_AgentRoundTrip -v`
Expected: FAIL

**Step 3: Extend Server to own and run the agent**

Add to `ServerConfig`:

```go
type ServerConfig struct {
	Version      string
	GracePeriod  time.Duration
	LLMClient    common.Client
	Guard        *guard.Guard
	Registry     *tools.Registry
	Settings     config.Settings
	SystemPrompt string
	MaxTokens    int
	ThinkingBudget int
	Home         string
	CWD          string
	SessionID    string
}
```

In `NewServer`, build the agent:

```go
func (s *Server) initAgent(cfg ServerConfig) {
	sess := session.NewSession(cfg.SessionID, cfg.CWD, "", "") // model set later
	path := session.SessionPath(cfg.Home, cfg.CWD, sess.ID, time.Now().UTC())
	w, _ := session.OpenWriter(path)

	// RPCFrontend is set when a client connects; use a placeholder.
	state := agent.NewSessionState(sess, w, cfg.LLMClient)

	s.agent = &agent.Agent{
		Tools:          cfg.Registry,
		Guard:          cfg.Guard,
		State:          state,
		System:         cfg.SystemPrompt,
		MaxTokens:      cfg.MaxTokens,
		ThinkingBudget: cfg.ThinkingBudget,
		Home:           cfg.Home,
	}
}
```

When `handleSessionInput` receives input, run the agent loop:

```go
func (s *Server) handleSessionInput(msg *rpc.Message) {
	var params rpc.SessionInputParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return
	}

	s.mu.Lock()
	fe := s.fe
	s.mu.Unlock()

	if fe == nil {
		return
	}

	// Set the RPCFrontend on the agent before running.
	s.agent.Frontend = fe
	fe.DeliverInput(params.Text)
}
```

The agent's `runLoop` (from current `main.go:99-162`) moves into the daemon. Start it when the first client connects. It calls `fe.AwaitUserInput` which blocks until `DeliverInput` is called.

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/daemon/ -run TestServer_AgentRoundTrip -v`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/daemon/server.go gohome/internal/daemon/server_test.go
git commit -m "feat(daemon): wire agent lifecycle to daemon server"
```

---

### Task 9: TUI Client Frontend

Rewrite `tui.Frontend` to be a JSON-RPC client that connects to the daemon. It receives `agent.event` notifications and translates them to `AgentEventMsg` for Bubble Tea. User input and approval decisions are sent as JSON-RPC requests.

**Files:**
- Modify: `gohome/internal/tui/frontend.go`
- Test: `gohome/internal/tui/frontend_test.go` (new file)

**Step 1: Write failing tests**

```go
package tui

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
)

func TestClientFrontend_ReceivesAgentEvent(t *testing.T) {
	c1, c2 := net.Pipe()
	daemonConn := rpc.NewConn(c1)
	clientConn := rpc.NewConn(c2)
	defer daemonConn.Close()
	defer clientConn.Close()

	events := make(chan AgentEventMsg, 10)
	fe := NewClientFrontend(clientConn, events)
	go fe.ReadLoop()

	// Daemon sends an agent.event notification.
	ev := agent.Event{Kind: agent.EventTokenDelta, SessionID: "s1", TextDelta: "hi"}
	params, _ := json.Marshal(rpc.AgentEventParams{SessionID: "s1", Event: ev})
	daemonConn.Notify(rpc.MethodAgentEvent, params)

	select {
	case msg := <-events:
		if msg.Ev.Kind != agent.EventTokenDelta {
			t.Errorf("kind: got %v", msg.Ev.Kind)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestClientFrontend_SendInput(t *testing.T) {
	c1, c2 := net.Pipe()
	daemonConn := rpc.NewConn(c1)
	clientConn := rpc.NewConn(c2)
	defer daemonConn.Close()
	defer clientConn.Close()

	fe := NewClientFrontend(clientConn, nil)

	done := make(chan struct{})
	go func() {
		defer close(done)
		msg, err := daemonConn.Read()
		if err != nil {
			t.Errorf("Read: %v", err)
			return
		}
		if msg.Method != rpc.MethodSessionInput {
			t.Errorf("method: got %q", msg.Method)
		}
		var p rpc.SessionInputParams
		json.Unmarshal(msg.Params, &p)
		if p.Text != "hello" {
			t.Errorf("text: got %q", p.Text)
		}
	}()

	if err := fe.SendInput("s1", "hello"); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	<-done
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/tui/ -run "TestClientFrontend" -v`
Expected: FAIL

**Step 3: Implement ClientFrontend**

Replace the contents of `gohome/internal/tui/frontend.go`:

```go
package tui

import (
	"encoding/json"
	"log/slog"
	"sync/atomic"

	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
)

// ClientFrontend connects the TUI to the daemon over JSON-RPC.
// It receives agent.event notifications and forwards them as AgentEventMsg.
// It sends user input and approval decisions as JSON-RPC requests.
type ClientFrontend struct {
	conn    *rpc.Conn
	events  chan<- AgentEventMsg
	pending *rpc.Pending
	idSeq   atomic.Int64
}

// NewClientFrontend creates a frontend connected to the daemon via conn.
// Agent events are pushed to the events channel for the Bubble Tea program.
func NewClientFrontend(conn *rpc.Conn, events chan<- AgentEventMsg) *ClientFrontend {
	return &ClientFrontend{
		conn:    conn,
		events:  events,
		pending: rpc.NewPending(),
	}
}

// ReadLoop reads messages from the daemon and dispatches them.
// Run this in a goroutine. It returns when the connection closes.
func (f *ClientFrontend) ReadLoop() {
	for {
		msg, err := f.conn.Read()
		if err != nil {
			slog.Debug("tui: daemon read error", "err", err)
			return
		}
		if msg.IsResponse() {
			if msg.ID != nil {
				f.pending.Resolve(msg.ID.Int64(), msg.Result, msg.Error)
			}
			continue
		}
		if msg.IsNotification() {
			f.handleNotification(msg)
			continue
		}
		if msg.IsRequest() {
			f.handleRequest(msg)
		}
	}
}

func (f *ClientFrontend) handleNotification(msg *rpc.Message) {
	switch msg.Method {
	case rpc.MethodAgentEvent:
		var params rpc.AgentEventParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			slog.Debug("tui: unmarshal agent event", "err", err)
			return
		}
		if f.events != nil {
			f.events <- AgentEventMsg{SessionID: params.SessionID, Ev: params.Event}
		}
	case rpc.MethodSessionState:
		// Reconnection state sync -- handle in a later task.
	}
}

func (f *ClientFrontend) handleRequest(msg *rpc.Message) {
	switch msg.Method {
	case rpc.MethodApprovalRequest:
		// The daemon is asking for user approval. Push an ApprovalReqMsg
		// to the Bubble Tea program. The response is sent when the user
		// resolves the overlay.
		var params rpc.ApprovalRequestParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return
		}
		// Store the request ID so we can respond later.
		// This will be wired when integrating with the TUI's approval overlay.
		_ = params
		_ = msg.ID
	}
}

// SendInput sends a session.input request to the daemon.
func (f *ClientFrontend) SendInput(sessionID, text string) error {
	params, err := json.Marshal(rpc.SessionInputParams{SessionID: sessionID, Text: text})
	if err != nil {
		return err
	}
	id := f.idSeq.Add(1)
	return f.conn.WriteRequest(rpc.Request{
		ID:     rpc.NewID(id),
		Method: rpc.MethodSessionInput,
		Params: params,
	})
}

// SendApproval sends a session.approval request to the daemon.
func (f *ClientFrontend) SendApproval(sessionID string, dec guard.ApprovalDecision) error {
	params, err := json.Marshal(rpc.SessionApprovalParams{SessionID: sessionID, Decision: dec})
	if err != nil {
		return err
	}
	id := f.idSeq.Add(1)
	return f.conn.WriteRequest(rpc.Request{
		ID:     rpc.NewID(id),
		Method: rpc.MethodSessionApproval,
		Params: params,
	})
}

// SendCancel sends a session.cancel request to the daemon.
func (f *ClientFrontend) SendCancel(sessionID string) error {
	params, err := json.Marshal(rpc.SessionCancelParams{SessionID: sessionID})
	if err != nil {
		return err
	}
	id := f.idSeq.Add(1)
	return f.conn.WriteRequest(rpc.Request{
		ID:     rpc.NewID(id),
		Method: rpc.MethodSessionCancel,
		Params: params,
	})
}

// Close closes the connection to the daemon.
func (f *ClientFrontend) Close() error {
	return f.conn.Close()
}
```

Remove the old `Frontend` struct, `NewFrontend`, `SetProgram`, `InputCh`, and the old `Emit`/`RequestApproval`/`AwaitUserInput` methods. The compile-time assertions `var _ agent.Frontend = (*Frontend)(nil)` and `var _ guard.Frontend = (*Frontend)(nil)` should be removed -- the TUI frontend is no longer an `agent.Frontend`. Update import references.

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/tui/ -run "TestClientFrontend" -v`
Expected: PASS

Then run all TUI tests to check for breakage:

Run: `go test ./gohome/internal/tui/ -v`
Expected: Some tests will fail because they reference the old `Frontend` type. Fix them in the next step.

**Step 5: Fix existing TUI tests**

Tests that create `NewFrontend()` and use `fe.input` or `fe.SetProgram` need updating. Most TUI snapshot tests construct a `Model` with `New(nil, "main")` and don't use the frontend at all -- these should pass unchanged. Tests that do need a frontend should use `NewClientFrontend` with a `net.Pipe()`.

**Step 6: Commit**

```bash
git add gohome/internal/tui/frontend.go gohome/internal/tui/frontend_test.go
git commit -m "feat(tui): rewrite Frontend as JSON-RPC client to daemon"
```

---

### Task 10: Update TUI Model to Use ClientFrontend

Wire the TUI `Model` to use `ClientFrontend` instead of the old `Frontend`. The `Model` no longer needs an `inputCh` field -- input submission goes through `ClientFrontend.SendInput`. The `AgentEventMsg` channel feeds into the Bubble Tea program via a subscription.

**Files:**
- Modify: `gohome/internal/tui/model.go` (update `New`, remove `inputCh` field)
- Modify: `gohome/internal/tui/model_agent.go` (update `sendInputCmd`)
- Modify: `gohome/internal/tui/model_keys.go` (update input submission)
- Modify: `gohome/internal/tui/model_approval.go` (send approval via RPC)
- Modify: `gohome/internal/tui/model_slash.go` (update slash command callbacks)

**Step 1: Update Model construction**

In `model.go`, change `New` to accept a `*ClientFrontend` instead of `*Frontend`:

```go
func New(fe *ClientFrontend, sessionID string) *Model {
```

Store the `ClientFrontend` on the Model so it can call `SendInput`, `SendApproval`, etc.

Add a field:

```go
type Model struct {
	// ... existing fields ...
	fe *ClientFrontend
}
```

Remove `inputCh chan string` from the Model struct.

**Step 2: Update sendInputCmd**

In `model_agent.go:324-330`, `sendInputCmd` currently pushes to `m.inputCh`. Change it to call `m.fe.SendInput`:

```go
func (m *Model) sendInputCmd(text string) tea.Cmd {
	fe := m.fe
	sid := m.focused
	return func() tea.Msg {
		if fe != nil {
			fe.SendInput(sid, text)
		}
		return nil
	}
}
```

**Step 3: Update key handler for Enter (input submission)**

In `model_keys.go`, where the user presses Enter and the editor content is sent, change from `m.inputCh <- text` to `m.fe.SendInput(m.focused, text)`.

**Step 4: Update approval handling**

In `model_approval.go`, when the user resolves an approval prompt, send the decision via `m.fe.SendApproval` instead of writing to the `reply` channel. The `ApprovalReqMsg` struct will change -- instead of carrying a `Reply chan guard.ApprovalDecision`, it will carry the `rpc.ID` of the daemon's request so the TUI can respond.

**Step 5: Update slash command callbacks**

`SlashCallbacks` (`ListSessions`, `ResumeSession`, `NewSession`, `SetModel`, `CancelSession`) currently call functions wired in `main.go`. In daemon mode, these become JSON-RPC requests. Change `SlashCallbacks` to call methods on `ClientFrontend` instead. For example:

```go
CancelSession: func(id string) {
	m.fe.SendCancel(id)
},
```

**Step 6: Run all TUI tests**

Run: `go test ./gohome/internal/tui/ -v`
Expected: PASS (after fixing test helpers)

**Step 7: Commit**

```bash
git add gohome/internal/tui/model.go gohome/internal/tui/model_agent.go \
       gohome/internal/tui/model_keys.go gohome/internal/tui/model_approval.go \
       gohome/internal/tui/model_slash.go
git commit -m "feat(tui): wire Model to ClientFrontend for JSON-RPC communication"
```

---

### Task 11: Update spawn.go for Daemon Mode

Update `spawn.go` so child agents get their own `RPCFrontend` instance. The daemon server's `RPCFrontend` is already tagged by `sessionID` in each `Emit` call, so the child just needs a reference to the same `RPCFrontend` (it already calls `Emit(childID, ...)` which tags correctly).

**Files:**
- Modify: `gohome/internal/agent/spawn.go:80` (Frontend assignment)
- Test: `gohome/internal/agent/spawn_test.go` (verify child events carry childID)

**Step 1: Review what needs to change**

Looking at `spawn.go:80`:

```go
childAgent := &Agent{
	// ...
	Frontend:       a.Frontend,  // shares parent Frontend
	// ...
}
```

In daemon mode, `a.Frontend` is an `RPCFrontend`. When the child calls `a.Frontend.Emit(childID, ev)`, the `sessionID` parameter is `childID`, which is correct. The `RPCFrontend.Emit` method sends a JSON-RPC notification with `sessionID=childID`. The TUI routes by `sessionID`.

**This means spawn.go needs no change for the basic case.** The parent and child share the same `RPCFrontend`, but events are already tagged with different `sessionID` values.

The only consideration: `RequestApproval` for child tool calls. The child's `Guard.Check` will call `RPCFrontend.RequestApproval`, which sends an `approval.request` to the TUI. The TUI needs to know which session the approval is for. The `ApprovalRequest.SessionID` field already carries this.

**Step 2: Write a test to verify child events carry the correct sessionID**

Add to `spawn_test.go`:

```go
func TestSpawn_ChildEventsCarryChildID(t *testing.T) {
	// Verify that when a child agent runs, events emitted to the Frontend
	// have sessionID set to the child's ID, not the parent's.
	// ... (use fakeRecorder, check ev.SessionID for child events)
}
```

**Step 3: Run test**

Run: `go test ./gohome/internal/agent/ -run TestSpawn_ChildEventsCarryChildID -v`
Expected: PASS (spawn.go already passes childID to Emit)

**Step 4: Commit (if any changes were needed)**

```bash
git commit -m "test(agent): verify subagent events carry child session ID"
```

---

### Task 12: Rewrite main.go

Rewrite `cmd/gohome/main.go` to implement the daemon-only startup flow:

1. Parse flags, load config
2. Check for existing daemon socket
3. If no daemon: start daemon (fork or in-process for now), then connect
4. If daemon exists: connect directly
5. Run TUI client loop

**Files:**
- Modify: `gohome/cmd/gohome/main.go`
- Test: manual testing (this is the integration point)

**Step 1: Refactor main.go**

The current `main.go` (~530 lines) does everything inline. Split into:

1. `main()` -- parse flags, detect daemon, connect or start
2. `startDaemon(cfg)` -- start the daemon server in a background goroutine (or fork)
3. `runClient(sockPath)` -- connect to daemon, run TUI

```go
func main() {
	flag.Parse()

	if *showVersion {
		fmt.Println("gohome " + version)
		return
	}

	home, cwd := resolveDirectories()
	logFile := setupLoggingOrDie(home)
	settings := loadSettingsOrDie(home, cwd)
	sockPath := filepath.Join(home, "daemon.sock")

	if *stopFlag {
		stopDaemon(sockPath)
		return
	}

	// Check for running daemon.
	if !isDaemonRunning(sockPath) {
		go startDaemon(sockPath, home, cwd, settings)
		waitForDaemon(sockPath, 5*time.Second)
	}

	runClient(sockPath, settings)

	if logFile != nil {
		_ = logFile.Close()
	}
}
```

`isDaemonRunning` connects to the socket and sends `daemon.health`. If it gets a response, the daemon is alive.

`startDaemon` creates a `daemon.Server` with the full config (LLM client, guard, tools, etc.) and calls `srv.Serve()`.

`runClient` connects to the socket, creates a `ClientFrontend`, builds the TUI `Model`, and runs `tea.NewProgram(m).Run()`.

**Step 2: Implement helper functions**

```go
func isDaemonRunning(sockPath string) bool {
	conn, err := net.DialTimeout("unix", sockPath, time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()
	c := rpc.NewConn(conn)
	params, _ := json.Marshal(struct{}{})
	c.WriteRequest(rpc.Request{
		ID:     rpc.NewID(1),
		Method: rpc.MethodDaemonHealth,
		Params: params,
	})
	msg, err := c.Read()
	if err != nil {
		return false
	}
	return msg.IsResponse() && msg.Error == nil
}

func waitForDaemon(sockPath string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isDaemonRunning(sockPath) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Fprintf(os.Stderr, "gohome: daemon did not start within %s\n", timeout)
	os.Exit(1)
}

func stopDaemon(sockPath string) {
	conn, err := net.DialTimeout("unix", sockPath, time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gohome: no daemon running\n")
		return
	}
	defer conn.Close()
	c := rpc.NewConn(conn)
	params, _ := json.Marshal(struct{}{})
	c.WriteRequest(rpc.Request{
		ID:     rpc.NewID(1),
		Method: rpc.MethodDaemonStop,
		Params: params,
	})
	fmt.Println("gohome: daemon stopped")
}
```

**Step 3: Implement runClient**

```go
func runClient(sockPath string, settings config.Settings) {
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gohome: cannot connect to daemon: %v\n", err)
		os.Exit(1)
	}

	c := rpc.NewConn(conn)
	eventCh := make(chan AgentEventMsg, 64)
	fe := tui.NewClientFrontend(c, eventCh)

	go fe.ReadLoop()

	m := tui.New(fe, "main")
	m.SetSettings(settings)
	// ... set other model properties from settings ...

	p := tea.NewProgram(m, tea.WithAltScreen())

	// Feed agent events from the channel into Bubble Tea.
	go func() {
		for ev := range eventCh {
			p.Send(ev)
		}
	}()

	if _, err := p.Run(); err != nil {
		slog.Error("tui error", "err", err)
	}

	fe.Close()
}
```

**Step 4: Add --stop flag**

```go
var stopFlag = flag.Bool("stop", false, "stop the running daemon and exit")
```

**Step 5: Run and test manually**

Build and run:
```bash
go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome
./bin/gohome --model <name>
```

Verify:
- TUI appears
- Typing a message sends it to the daemon, response streams back
- `./bin/gohome --stop` stops the daemon
- Running `./bin/gohome` again after stop starts a new daemon

**Step 6: Commit**

```bash
git add gohome/cmd/gohome/main.go
git commit -m "feat: rewrite main.go for daemon-only architecture"
```

---

### Task 13: Implement Remaining Server RPC Handlers

Wire up the session management RPC handlers in the daemon server: `session.new`, `session.resume`, `session.list`, `session.cancel`, `model.set`.

**Files:**
- Modify: `gohome/internal/daemon/server.go`
- Test: `gohome/internal/daemon/server_test.go`

**Step 1: Write failing tests for each handler**

```go
func TestServer_SessionList(t *testing.T) {
	// Connect, send session.list, verify response contains sessions array.
}

func TestServer_SessionNew(t *testing.T) {
	// Connect, send session.new, verify new sessionID is returned.
}

func TestServer_SessionCancel(t *testing.T) {
	// Start a long-running agent turn, send session.cancel, verify it stops.
}

func TestServer_ModelSet(t *testing.T) {
	// Send model.set with a valid model name, verify response.
}
```

**Step 2: Implement handlers**

Each handler mirrors the logic currently in `main.go`'s `SlashCallbacks`:

- `session.list`: Call `session.List(home, cwd)` and return the listings.
- `session.new`: Create a new session, swap state, return new session ID.
- `session.resume`: Load session from JSONL, swap state, return session ID + history.
- `session.cancel`: Cancel the agent context.
- `model.set`: Look up model config, create new LLM client, update state.

**Step 3: Run tests**

Run: `go test ./gohome/internal/daemon/ -v`
Expected: PASS

**Step 4: Commit**

```bash
git add gohome/internal/daemon/server.go gohome/internal/daemon/server_test.go
git commit -m "feat(daemon): implement session management and model switch RPC handlers"
```

---

### Task 14: Approval Flow End-to-End

Wire the approval flow: daemon sends `approval.request` to TUI, TUI shows overlay, user decides, TUI sends response back.

**Files:**
- Modify: `gohome/internal/tui/frontend.go` (handle incoming `approval.request`)
- Modify: `gohome/internal/tui/model_approval.go` (resolve approval via RPC response)
- Test: `gohome/internal/daemon/server_test.go` (integration test)

**Step 1: Handle approval.request in ClientFrontend.ReadLoop**

When the TUI's `ReadLoop` receives an `approval.request` from the daemon, it sends an `ApprovalReqMsg` to the Bubble Tea program (via `p.Send`). The `ApprovalReqMsg` now carries the RPC request ID instead of a reply channel:

```go
type ApprovalReqMsg struct {
	Req     guard.ApprovalRequest
	RPCID   *rpc.ID // ID of the daemon's request, used to send the response
}
```

When the user resolves the approval overlay, the TUI sends a JSON-RPC response back to the daemon with the matching ID.

**Step 2: Write integration test**

Test that: daemon runs agent -> agent hits non-whitelisted tool -> daemon sends approval.request -> test client responds with AllowOnce -> agent proceeds.

**Step 3: Implement**

In `ClientFrontend.handleRequest`:
```go
case rpc.MethodApprovalRequest:
	var params rpc.ApprovalRequestParams
	json.Unmarshal(msg.Params, &params)
	req := guard.ApprovalRequest{
		SessionID:        params.SessionID,
		Tool:             params.Tool,
		Input:            params.Input,
		Summary:          params.Summary,
		SuggestedPattern: params.SuggestedPattern,
	}
	// Send to Bubble Tea program. The Model's handleApprovalReq will
	// eventually call RespondApproval with the decision.
	f.approvalCh <- ApprovalReqMsg{Req: req, RPCID: msg.ID}
```

Add `RespondApproval` method:
```go
func (f *ClientFrontend) RespondApproval(id *rpc.ID, dec guard.ApprovalDecision) error {
	result, _ := json.Marshal(rpc.ApprovalResponseResult{Decision: dec})
	return f.conn.WriteResponse(rpc.Response{ID: id, Result: result})
}
```

**Step 4: Run tests**

Run: `go test ./gohome/internal/daemon/ -v && go test ./gohome/internal/tui/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/tui/frontend.go gohome/internal/tui/model_approval.go \
       gohome/internal/daemon/server.go gohome/internal/daemon/server_test.go
git commit -m "feat: wire approval flow end-to-end through JSON-RPC"
```

---

### Task 15: Reconnection and State Sync

When a new TUI client connects to a running daemon, the daemon sends a `session.state` notification with the current session state so the TUI can reconstruct its view.

**Files:**
- Modify: `gohome/internal/daemon/server.go` (send state on connect)
- Modify: `gohome/internal/tui/frontend.go` (handle `session.state`)
- Test: `gohome/internal/daemon/server_test.go`

**Step 1: Write failing test**

```go
func TestServer_Reconnect_SendsState(t *testing.T) {
	// Start daemon, connect client 1, send a message to populate timeline,
	// disconnect client 1, connect client 2, verify client 2 receives
	// session.state with the timeline.
}
```

**Step 2: Implement state snapshot**

In `server.go`, when a new client connects in `handleClient`, build and send a `SessionStateParams`:

```go
func (s *Server) sendStateSync(c *rpc.Conn) {
	sess := s.agent.State.Session()
	// Build timeline from session history (or from a cached timeline).
	params := rpc.SessionStateParams{
		SessionID: sess.ID,
		Model:     sess.Model,
		Yolo:      s.guard.IsYolo(),
		// Timeline built from session history
	}
	data, _ := json.Marshal(params)
	c.Notify(rpc.MethodSessionState, data)
}
```

In `ClientFrontend.handleNotification`, handle `session.state` by building timeline entries and feeding them to Bubble Tea.

**Step 3: Run tests**

Run: `go test ./gohome/internal/daemon/ -v`
Expected: PASS

**Step 4: Commit**

```bash
git add gohome/internal/daemon/server.go gohome/internal/tui/frontend.go \
       gohome/internal/daemon/server_test.go
git commit -m "feat(daemon): send session state on TUI reconnect"
```

---

### Task 16: Grace Period and Auto-Shutdown

When the TUI disconnects and no turn is in-flight, the daemon should exit after a grace period (default 30s).

**Files:**
- Modify: `gohome/internal/daemon/server.go`
- Test: `gohome/internal/daemon/server_test.go`

**Step 1: Write failing test**

```go
func TestServer_GracePeriod_ExitsWhenIdle(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")

	srv, err := NewServer(sock, ServerConfig{
		Version:     "test",
		GracePeriod: 100 * time.Millisecond, // short for testing
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	done := make(chan struct{})
	go func() {
		srv.Serve()
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	// Connect and immediately disconnect.
	conn, _ := net.Dial("unix", sock)
	conn.Close()

	// Server should exit within the grace period.
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("server did not exit within grace period")
	}
}
```

**Step 2: Implement grace period**

After `handleClient` returns (client disconnected), start a timer. If no new client connects within the grace period and no agent turn is in-flight, call `s.Stop()`.

**Step 3: Run tests**

Run: `go test ./gohome/internal/daemon/ -v`
Expected: PASS

**Step 4: Commit**

```bash
git add gohome/internal/daemon/server.go gohome/internal/daemon/server_test.go
git commit -m "feat(daemon): auto-shutdown after grace period when idle"
```

---

### Task 17: Architecture Seam Test

Extend the existing seam test (`gohome/internal/agent/seam_test.go`) to verify that the `daemon` package does not import `tui`, and that `tui` does not import `agent` (it only imports `daemon/rpc` for protocol types).

**Files:**
- Modify: `gohome/internal/agent/seam_test.go`

**Step 1: Add daemon seam checks**

```go
// Add to the pkgs list:
module + "/gohome/internal/daemon",
module + "/gohome/internal/daemon/rpc",
module + "/gohome/internal/daemon/frontend",
```

**Step 2: Run test**

Run: `go test ./gohome/internal/agent/ -run TestNoTUIImport -v`
Expected: PASS

**Step 3: Commit**

```bash
git add gohome/internal/agent/seam_test.go
git commit -m "test(agent): extend seam test to cover daemon packages"
```

---

### Task 18: Update FUTURE.md

Mark daemon mode as DELIVERED and update the description.

**Files:**
- Modify: `gohome/FUTURE.md:8-14`

**Step 1: Update the entry**

Change:
```markdown
**Daemon mode**
Run the agent as a background process, with a thin TUI client connecting over
a Unix socket or stdin/stdout JSON-RPC.
Seam: ...
```

To:
```markdown
**Daemon mode** -- DELIVERED in v0.3.0
The agent runs as a background daemon process communicating with the TUI
over JSON-RPC 2.0 on a Unix socket (`~/.gohome/daemon.sock`). Subagents
are goroutines in the daemon. The TUI is a thin Bubble Tea client that
receives events and sends requests over the same connection.
```

**Step 2: Commit**

```bash
git add gohome/FUTURE.md
git commit -m "docs: mark daemon mode as delivered"
```

---

### Task 19: Full Integration Test

Run the full test suite and fix any remaining failures.

**Step 1: Run all tests**

```bash
go test ./gohome/... -v
```

**Step 2: Run vet and lint**

```bash
go vet ./gohome/...
golangci-lint run ./gohome/...
```

**Step 3: Build and manual smoke test**

```bash
go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome
./bin/gohome --model <name>
```

Test:
- Normal conversation works
- Subagent spawning works (tabs appear, child events stream)
- Approval prompts work
- `/new`, `/resume`, `/model`, `/cancel` slash commands work
- `Ctrl+C` cancels in-flight turn
- Disconnecting and reconnecting shows previous state
- `./bin/gohome --stop` stops the daemon
- `./bin/gohome` after stop starts fresh

**Step 4: Fix any failures and commit**

```bash
git commit -m "fix: address test and integration issues from daemon mode migration"
```

---

### Task 20: Update Snapshot Golden Files

TUI snapshot tests may produce different output due to structural changes (removed `inputCh`, new `ClientFrontend`). Regenerate golden files.

**Step 1: Run snapshot tests with -update**

```bash
go test ./gohome/internal/tui/ -run TestSnapshots -update
```

**Step 2: Review diffs**

```bash
git diff gohome/internal/tui/testdata/
```

Verify changes are expected (no regressions in rendered output).

**Step 3: Commit**

```bash
git add gohome/internal/tui/testdata/
git commit -m "test(tui): update snapshot golden files for daemon mode"
```
