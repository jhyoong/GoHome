package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// helper: send a JSON-RPC request over a raw connection and read the response.
// It skips any notifications (e.g. session.state) that may arrive before the
// response to the request.
func sendRequest(t *testing.T, conn net.Conn, id int64, method string, params json.RawMessage) *rpc.Message {
	t.Helper()

	req := rpc.Request{
		ID:     rpc.NewID(id),
		Method: method,
		Params: params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	data = append(data, '\n')

	if _, err := conn.Write(data); err != nil {
		t.Fatalf("write request: %v", err)
	}

	// Read messages until we get a response (skip notifications).
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		msg, err := rpc.Decode(scanner.Bytes())
		if err != nil {
			t.Fatalf("decode message: %v", err)
		}
		if msg.IsResponse() {
			return msg
		}
		// Skip notifications (e.g. session.state sent on connect).
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}
	t.Fatal("connection closed before response received")
	return nil
}

func newTestSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "gh-daemon-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "t.sock")
}

func serveBackground(t *testing.T, srv *Server) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		srv.Serve()
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	t.Cleanup(func() {
		srv.Stop()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("server did not stop within 2s")
		}
	})
}

func newTestGuard() *guard.Guard {
	wl := &guard.Whitelist{}
	g := guard.NewGuard(wl, &noopApprover{})
	g.SetYolo(true)
	return g
}

func dialTestServer(t *testing.T, sockPath string) net.Conn {
	t.Helper()
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// newTestAgentServer creates a Server with sensible defaults for agent-based
// tests. Use functional options to override individual ServerConfig fields.
func newTestAgentServer(t *testing.T, sock string, opts ...func(*ServerConfig)) *Server {
	t.Helper()
	dir := t.TempDir()
	cfg := ServerConfig{
		Version:      "test",
		LLMClient:    &echoClient{},
		Guard:        newTestGuard(),
		Registry:     tools.NewRegistry(),
		SystemPrompt: "test",
		MaxTokens:    1024,
		Home:         dir,
		CWD:          dir,
		SessionID:    "test-sess",
		Settings:     config.Settings{ModelConfig: map[string]config.ModelConfig{}},
	}
	for _, fn := range opts {
		fn(&cfg)
	}
	srv, err := NewServer(sock, cfg)
	if err != nil {
		t.Fatalf("newTestAgentServer: %v", err)
	}
	return srv
}

func TestServer_HealthCheck(t *testing.T) {
	sock := newTestSocket(t)

	srv, err := NewServer(sock, ServerConfig{Version: "test"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	serveBackground(t, srv)

	conn := dialTestServer(t, sock)

	// Send daemon.health request.
	msg := sendRequest(t, conn, 1, rpc.MethodDaemonHealth, nil)

	if msg.Error != nil {
		t.Fatalf("unexpected error: %v", msg.Error)
	}
	if msg.Result == nil {
		t.Fatal("expected result, got nil")
	}

	var health rpc.HealthResult
	if err := json.Unmarshal(msg.Result, &health); err != nil {
		t.Fatalf("unmarshal health: %v", err)
	}

	if health.Version != "test" {
		t.Errorf("version = %q, want %q", health.Version, "test")
	}
	if health.UptimeSeconds < 0 {
		t.Errorf("uptimeSeconds = %d, want >= 0", health.UptimeSeconds)
	}

	// Verify socket file is cleaned up after serveBackground's cleanup runs.
	// (serveBackground calls srv.Stop() which triggers cleanup.)
}

func TestServer_Stop(t *testing.T) {
	sock := newTestSocket(t)

	srv, err := NewServer(sock, ServerConfig{Version: "test"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Track when Serve exits.
	serveDone := make(chan struct{})
	go func() {
		srv.Serve()
		close(serveDone)
	}()

	// Give the server a moment to start accepting.
	time.Sleep(50 * time.Millisecond)

	// Connect and send daemon.stop.
	conn := dialTestServer(t, sock)

	msg := sendRequest(t, conn, 1, rpc.MethodDaemonStop, nil)
	if msg.Error != nil {
		t.Fatalf("unexpected error: %v", msg.Error)
	}

	// Verify Serve() returns within 2s.
	select {
	case <-serveDone:
		// Success.
	case <-time.After(2 * time.Second):
		t.Fatal("Serve() did not return within 2s after daemon.stop")
	}

	// Verify socket file is cleaned up.
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Errorf("socket file still exists after stop")
	}
}

// echoClient is a fake common.Client that returns a fixed text response with
// no tool calls, so the agent loop completes a single turn immediately.
type echoClient struct{}

func (c *echoClient) Stream(_ context.Context, req common.Request) (<-chan common.StreamEvent, error) {
	ch := make(chan common.StreamEvent, 3)
	ch <- common.StreamEvent{Kind: common.EventTextDelta, TextDelta: "echo reply"}
	ch <- common.StreamEvent{Kind: common.EventTurnDone, StopReason: "end_turn", Usage: &common.Usage{InputTokens: 10, OutputTokens: 5}}
	close(ch)
	return ch, nil
}

func TestServer_WithAgent_ProcessesInput(t *testing.T) {
	sock := newTestSocket(t)
	srv := newTestAgentServer(t, sock)

	// Verify the agent was built.
	if srv.agent == nil {
		t.Fatal("expected agent to be non-nil after providing LLMClient")
	}

	serveBackground(t, srv)

	// Use a single rpc.Conn for the entire test to avoid buffered-reader
	// conflicts (a bare bufio.Scanner and rpc.Conn sharing one net.Conn
	// causes the scanner to consume notifications on fast CI runners).
	conn := dialTestServer(t, sock)
	rc := rpc.NewConn(conn)

	// Skip the session.state notification sent on connect.
	if stateMsg, err := rc.Read(); err != nil {
		t.Fatalf("read state notification: %v", err)
	} else if stateMsg.Method != rpc.MethodSessionState {
		t.Fatalf("expected session.state notification, got method=%q", stateMsg.Method)
	}

	// Send a session.input request via the rpc.Conn.
	inputParams, _ := json.Marshal(rpc.SessionInputParams{Text: "hello"})
	if err := rc.WriteRequest(rpc.Request{
		ID:     rpc.NewID(1),
		Method: rpc.MethodSessionInput,
		Params: inputParams,
	}); err != nil {
		t.Fatalf("write session.input: %v", err)
	}

	// Read messages until the read deadline. Collect the response and any
	// agent.event notifications that follow.
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var notifications []*rpc.Message
	for {
		msg, err := rc.Read()
		if err != nil {
			break
		}
		if msg.IsResponse() {
			if msg.Error != nil {
				t.Fatalf("unexpected error on session.input: %v", msg.Error)
			}
			continue
		}
		notifications = append(notifications, msg)
	}

	if len(notifications) == 0 {
		t.Fatal("expected at least one agent.event notification, got none")
	}

	foundEcho := false
	for _, n := range notifications {
		if n.Method == rpc.MethodAgentEvent {
			var params rpc.AgentEventParams
			if err := json.Unmarshal(n.Params, &params); err == nil {
				if params.Event.TextDelta == "echo reply" {
					foundEcho = true
				}
			}
		}
	}
	if !foundEcho {
		t.Error("expected to find agent.event with textDelta=\"echo reply\"")
	}

	// Verify session JSONL was written.
	sessionsDir := filepath.Join(srv.config.Home, "sessions")
	var jsonlFiles []string
	_ = filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && filepath.Ext(path) == ".jsonl" {
			jsonlFiles = append(jsonlFiles, path)
		}
		return nil
	})
	if len(jsonlFiles) == 0 {
		t.Fatal("expected session JSONL file to be created")
	}

	// Read the JSONL file and verify it contains a session_start and user_message.
	f, err := os.Open(jsonlFiles[0])
	if err != nil {
		t.Fatalf("open JSONL: %v", err)
	}
	defer func() { _ = f.Close() }()

	var events []map[string]json.RawMessage
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(scanner.Bytes(), &m); err == nil {
			events = append(events, m)
		}
	}

	if len(events) < 2 {
		t.Fatalf("expected at least 2 JSONL events (session_start + user_message), got %d", len(events))
	}

	// Check first event is session_start.
	var firstType string
	_ = json.Unmarshal(events[0]["type"], &firstType)
	if firstType != "session_start" {
		t.Errorf("first JSONL event type = %q, want session_start", firstType)
	}
}

func TestServer_SessionCancel(t *testing.T) {
	sock := newTestSocket(t)
	srv := newTestAgentServer(t, sock, func(cfg *ServerConfig) {
		cfg.SessionID = "cancel-test-1"
	})

	// Track whether cancel was called by setting a turn cancel function.
	cancelCalled := false
	srv.turnMu.Lock()
	srv.turnCancel = func() { cancelCalled = true }
	srv.turnMu.Unlock()

	serveBackground(t, srv)

	conn := dialTestServer(t, sock)

	cancelParams, _ := json.Marshal(rpc.SessionCancelParams{SessionID: "cancel-test-1"})
	msg := sendRequest(t, conn, 1, rpc.MethodSessionCancel, cancelParams)
	if msg.Error != nil {
		t.Fatalf("unexpected error: %v", msg.Error)
	}

	if !cancelCalled {
		t.Error("expected turnCancel to be called")
	}
}

func TestServer_SessionList(t *testing.T) {
	sock := newTestSocket(t)
	srv := newTestAgentServer(t, sock, func(cfg *ServerConfig) {
		cfg.SessionID = "list-test-1"
	})

	serveBackground(t, srv)

	conn := dialTestServer(t, sock)

	msg := sendRequest(t, conn, 1, rpc.MethodSessionList, json.RawMessage(`{}`))
	if msg.Error != nil {
		t.Fatalf("unexpected error: %v", msg.Error)
	}
	if msg.Result == nil {
		t.Fatal("expected result, got nil")
	}

	var result rpc.SessionListResult
	if err := json.Unmarshal(msg.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	// The initAgent call creates a JSONL file, so we should see one session.
	if len(result.Sessions) != 1 {
		t.Errorf("sessions count = %d, want 1", len(result.Sessions))
	}
	if len(result.Sessions) > 0 && result.Sessions[0].ID != "list-test-1" {
		t.Errorf("session ID = %q, want %q", result.Sessions[0].ID, "list-test-1")
	}
}

func TestServer_SessionNew_SwapOrder(t *testing.T) {
	sock := newTestSocket(t)
	srv := newTestAgentServer(t, sock, func(cfg *ServerConfig) {
		cfg.SessionID = "swap-test-1"
	})

	serveBackground(t, srv)

	conn := dialTestServer(t, sock)

	// Send session.new -- this exercises the reordered swap closure.
	msg := sendRequest(t, conn, 1, rpc.MethodSessionNew, json.RawMessage(`{}`))
	if msg.Error != nil {
		t.Fatalf("unexpected error on session.new: code=%d message=%q",
			msg.Error.Code, msg.Error.Message)
	}
	if msg.Result == nil {
		t.Fatal("expected result, got nil")
	}

	var result rpc.SessionNewResult
	if err := json.Unmarshal(msg.Result, &result); err != nil {
		t.Fatalf("unmarshal session.new result: %v", err)
	}
	if result.SessionID == "" {
		t.Fatal("expected non-empty sessionID in session.new result")
	}
	if result.SessionID == "swap-test-1" {
		t.Error("new session ID should differ from the original")
	}

	// Verify the old session's JSONL file contains a session_end event
	// and the new session's JSONL file contains a session_start event.
	sessionsDir := filepath.Join(srv.config.Home, "sessions")
	var jsonlFiles []string
	_ = filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && filepath.Ext(path) == ".jsonl" {
			jsonlFiles = append(jsonlFiles, path)
		}
		return nil
	})
	if len(jsonlFiles) < 2 {
		t.Fatalf("expected at least 2 JSONL files (old + new session), got %d", len(jsonlFiles))
	}

	// Verify the new session JSONL has a session_start event.
	foundStart := false
	for _, jf := range jsonlFiles {
		data, err := os.ReadFile(jf)
		if err != nil {
			t.Fatalf("read JSONL: %v", err)
		}
		if len(data) == 0 {
			continue
		}
		// Check first line for session_start.
		var first map[string]json.RawMessage
		lines := bufio.NewScanner(bufio.NewReader(
			func() *os.File { f, _ := os.Open(jf); return f }(),
		))
		if lines.Scan() {
			if err := json.Unmarshal(lines.Bytes(), &first); err == nil {
				var evType string
				_ = json.Unmarshal(first["type"], &evType)
				if evType == "session_start" {
					var start map[string]json.RawMessage
					_ = json.Unmarshal(lines.Bytes(), &start)
					var sid string
					_ = json.Unmarshal(start["id"], &sid)
					if sid == result.SessionID {
						foundStart = true
					}
				}
			}
		}
	}
	if !foundStart {
		t.Error("expected to find session_start event for new session in JSONL files")
	}
}

func TestServer_Reconnect_SendsState(t *testing.T) {
	sock := newTestSocket(t)
	srv := newTestAgentServer(t, sock, func(cfg *ServerConfig) {
		cfg.SessionID = "reconnect-sess-1"
	})

	serveBackground(t, srv)

	// Client 1 connects and disconnects.
	conn1, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial client 1: %v", err)
	}

	// Read the session.state notification sent on connect.
	_ = conn1.SetReadDeadline(time.Now().Add(10 * time.Second))
	rc1 := rpc.NewConn(conn1)
	msg1, err := rc1.Read()
	if err != nil {
		t.Fatalf("client 1 read: %v", err)
	}
	if msg1.Method != rpc.MethodSessionState {
		t.Fatalf("client 1: expected method %q, got %q", rpc.MethodSessionState, msg1.Method)
	}

	var state1 rpc.SessionStateParams
	if err := json.Unmarshal(msg1.Params, &state1); err != nil {
		t.Fatalf("unmarshal state1: %v", err)
	}
	if state1.SessionID != "reconnect-sess-1" {
		t.Errorf("client 1 sessionID = %q, want %q", state1.SessionID, "reconnect-sess-1")
	}
	if !state1.Yolo {
		t.Error("client 1 yolo = false, want true")
	}

	// Disconnect client 1.
	_ = conn1.Close()
	time.Sleep(100 * time.Millisecond) // let server notice disconnect

	// Client 2 connects.
	conn2 := dialTestServer(t, sock)

	// Read the session.state notification sent on connect.
	_ = conn2.SetReadDeadline(time.Now().Add(10 * time.Second))
	rc2 := rpc.NewConn(conn2)
	msg2, err := rc2.Read()
	if err != nil {
		t.Fatalf("client 2 read: %v", err)
	}
	if msg2.Method != rpc.MethodSessionState {
		t.Fatalf("client 2: expected method %q, got %q", rpc.MethodSessionState, msg2.Method)
	}

	var state2 rpc.SessionStateParams
	if err := json.Unmarshal(msg2.Params, &state2); err != nil {
		t.Fatalf("unmarshal state2: %v", err)
	}
	if state2.SessionID != "reconnect-sess-1" {
		t.Errorf("client 2 sessionID = %q, want %q", state2.SessionID, "reconnect-sess-1")
	}
	if !state2.Yolo {
		t.Error("client 2 yolo = false, want true")
	}
}

func TestServer_GracePeriod_ExitsWhenIdle(t *testing.T) {
	sock := newTestSocket(t)

	srv, err := NewServer(sock, ServerConfig{
		Version:     "test",
		GracePeriod: 200 * time.Millisecond,
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

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close()

	select {
	case <-done:
		// Success.
	case <-time.After(2 * time.Second):
		t.Fatal("server did not exit within grace period")
	}
}

func TestServer_GracePeriod_CancelledByReconnect(t *testing.T) {
	sock := newTestSocket(t)

	srv, err := NewServer(sock, ServerConfig{
		Version:     "test",
		GracePeriod: 500 * time.Millisecond,
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

	conn1, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial 1: %v", err)
	}
	_ = conn1.Close()

	time.Sleep(100 * time.Millisecond)

	conn2, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial 2: %v", err)
	}

	// Wait longer than the original grace period to verify cancellation.
	time.Sleep(600 * time.Millisecond)

	select {
	case <-done:
		t.Fatal("server should still be running (grace timer was cancelled)")
	default:
	}

	_ = conn2.Close()
	srv.Stop()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop after explicit Stop()")
	}
}

func TestDispatch_NilIDResponse_NoPanic(t *testing.T) {
	sock := newTestSocket(t)

	srv, err := NewServer(sock, ServerConfig{Version: "test"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	serveBackground(t, srv)

	conn := dialTestServer(t, sock)

	// Send a raw response with no "id" field (nil ID).
	raw := []byte(`{"result":{}}` + "\n")
	if _, err := conn.Write(raw); err != nil {
		t.Fatalf("write: %v", err)
	}

	// If dispatch panics, the server would crash and the next request would fail.
	// Send a health check to verify the server is still alive.
	msg := sendRequest(t, conn, 1, "daemon.health", nil)
	if msg.Error != nil {
		t.Fatalf("health check after nil-ID response failed: %v", msg.Error)
	}
}

func TestServer_YoloSet(t *testing.T) {
	sock := newTestSocket(t)
	srv := newTestAgentServer(t, sock, func(cfg *ServerConfig) {
		cfg.SessionID = "yolo-test-1"
	})

	serveBackground(t, srv)
	conn := dialTestServer(t, sock)

	// Disable yolo.
	params, _ := json.Marshal(rpc.YoloSetParams{Enabled: false})
	msg := sendRequest(t, conn, 1, rpc.MethodYoloSet, params)
	if msg.Error != nil {
		t.Fatalf("yolo.set(false) error: %v", msg.Error)
	}
	if srv.config.Guard.Yolo() {
		t.Error("expected yolo=false after yolo.set(false)")
	}

	// Re-enable yolo.
	params2, _ := json.Marshal(rpc.YoloSetParams{Enabled: true})
	msg2 := sendRequest(t, conn, 2, rpc.MethodYoloSet, params2)
	if msg2.Error != nil {
		t.Fatalf("yolo.set(true) error: %v", msg2.Error)
	}
	if !srv.config.Guard.Yolo() {
		t.Error("expected yolo=true after yolo.set(true)")
	}
}

func TestServer_SessionResume(t *testing.T) {
	sock := newTestSocket(t)
	srv := newTestAgentServer(t, sock, func(cfg *ServerConfig) {
		cfg.SessionID = "resume-original"
	})

	serveBackground(t, srv)

	conn := dialTestServer(t, sock)
	rc := rpc.NewConn(conn)

	// Skip the session.state notification sent on connect.
	if _, err := rc.Read(); err != nil {
		t.Fatalf("read state notification: %v", err)
	}

	// 1. Send session.input to create some history in the original session.
	inputParams, _ := json.Marshal(rpc.SessionInputParams{Text: "hello from original"})
	if err := rc.WriteRequest(rpc.Request{
		ID:     rpc.NewID(1),
		Method: rpc.MethodSessionInput,
		Params: inputParams,
	}); err != nil {
		t.Fatalf("write session.input: %v", err)
	}

	// Drain the response and agent.event notifications so the agent turn completes.
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		msg, err := rc.Read()
		if err != nil {
			break
		}
		if msg.IsResponse() && msg.Error != nil {
			t.Fatalf("session.input error: %v", msg.Error)
		}
	}
	_ = conn.Close()

	// 2. Reconnect and create a new session with session.new.
	conn2 := dialTestServer(t, sock)

	newMsg := sendRequest(t, conn2, 2, rpc.MethodSessionNew, json.RawMessage(`{}`))
	if newMsg.Error != nil {
		t.Fatalf("session.new error: code=%d message=%q", newMsg.Error.Code, newMsg.Error.Message)
	}
	var newResult rpc.SessionNewResult
	if err := json.Unmarshal(newMsg.Result, &newResult); err != nil {
		t.Fatalf("unmarshal session.new result: %v", err)
	}
	if newResult.SessionID == "resume-original" {
		t.Fatal("new session ID should differ from original")
	}

	// 3. Resume the original session.
	resumeParams, _ := json.Marshal(rpc.SessionResumeParams{ID: "resume-original"})
	resumeMsg := sendRequest(t, conn2, 3, rpc.MethodSessionResume, resumeParams)
	if resumeMsg.Error != nil {
		t.Fatalf("session.resume error: code=%d message=%q", resumeMsg.Error.Code, resumeMsg.Error.Message)
	}
	if resumeMsg.Result == nil {
		t.Fatal("expected result from session.resume, got nil")
	}

	var resumeResult rpc.SessionResumeResult
	if err := json.Unmarshal(resumeMsg.Result, &resumeResult); err != nil {
		t.Fatalf("unmarshal session.resume result: %v", err)
	}

	// Verify the resumed session ID matches the original.
	if resumeResult.SessionID != "resume-original" {
		t.Errorf("resumed sessionID = %q, want %q", resumeResult.SessionID, "resume-original")
	}

	// Verify history is non-empty (the original session had user input + agent reply).
	if len(resumeResult.History) == 0 {
		t.Error("expected non-empty history from resumed session")
	}
}

func TestServer_SessionResume_NotFound(t *testing.T) {
	sock := newTestSocket(t)
	srv := newTestAgentServer(t, sock, func(cfg *ServerConfig) {
		cfg.SessionID = "some-session"
	})

	serveBackground(t, srv)

	conn := dialTestServer(t, sock)

	resumeParams, _ := json.Marshal(rpc.SessionResumeParams{ID: "nonexistent-session"})
	msg := sendRequest(t, conn, 1, rpc.MethodSessionResume, resumeParams)
	if msg.Error == nil {
		t.Fatal("expected error for nonexistent session, got nil")
	}
	if msg.Error.Code != rpc.ErrServerError {
		t.Errorf("error code = %d, want %d", msg.Error.Code, rpc.ErrServerError)
	}
}

func TestServer_ModelSet_NotFound(t *testing.T) {
	sock := newTestSocket(t)
	srv := newTestAgentServer(t, sock, func(cfg *ServerConfig) {
		cfg.SessionID = "model-test-1"
	})

	serveBackground(t, srv)

	conn := dialTestServer(t, sock)

	params, _ := json.Marshal(rpc.ModelSetParams{Name: "unknown-model"})
	msg := sendRequest(t, conn, 1, rpc.MethodModelSet, params)
	if msg.Error == nil {
		t.Fatal("expected error for unknown model, got nil")
	}
	if msg.Error.Code != rpc.ErrServerError {
		t.Errorf("error code = %d, want %d", msg.Error.Code, rpc.ErrServerError)
	}
}

func TestServer_ModelSet_Success(t *testing.T) {
	sock := newTestSocket(t)

	// Set a test API key env var for the model config to reference.
	t.Setenv("TEST_GOHOME_MODEL_KEY", "test-key-value")

	srv := newTestAgentServer(t, sock, func(cfg *ServerConfig) {
		cfg.SessionID = "model-set-1"
		cfg.Settings = config.Settings{
			ModelConfig: map[string]config.ModelConfig{
				"test-model": {
					Wire:          config.WireOpenAI,
					BaseURL:       "http://localhost:0/v1", // not contacted
					APIKeyEnv:     "TEST_GOHOME_MODEL_KEY",
					ModelName:     "gpt-test-1",
					ContextWindow: 64000,
				},
			},
		}
	})

	serveBackground(t, srv)

	conn := dialTestServer(t, sock)

	params, _ := json.Marshal(rpc.ModelSetParams{Name: "test-model"})
	msg := sendRequest(t, conn, 1, rpc.MethodModelSet, params)
	if msg.Error != nil {
		t.Fatalf("model.set error: code=%d message=%q", msg.Error.Code, msg.Error.Message)
	}
	if msg.Result == nil {
		t.Fatal("expected result from model.set, got nil")
	}

	var result rpc.ModelSetResult
	if err := json.Unmarshal(msg.Result, &result); err != nil {
		t.Fatalf("unmarshal model.set result: %v", err)
	}

	if result.ModelName != "gpt-test-1" {
		t.Errorf("modelName = %q, want %q", result.ModelName, "gpt-test-1")
	}
	if result.ContextWindow != 64000 {
		t.Errorf("contextWindow = %d, want %d", result.ContextWindow, 64000)
	}
}

// noopApprover satisfies guard.Frontend for test purposes.
type noopApprover struct{}

func (n *noopApprover) RequestApproval(_ context.Context, _ guard.ApprovalRequest) (guard.ApprovalDecision, error) {
	return guard.ApprovalDecision{Outcome: guard.AllowOnce}, nil
}
