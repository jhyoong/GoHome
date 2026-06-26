package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// helper: send a JSON-RPC request over a raw connection and read the response.
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

	// Read response (newline-delimited).
	rc := rpc.NewConn(conn)
	msg, err := rc.Read()
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return msg
}

func TestServer_HealthCheck(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")

	srv, err := NewServer(sock, ServerConfig{Version: "test"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Start Serve in a goroutine.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		srv.Serve()
	}()

	// Give the server a moment to start accepting.
	time.Sleep(50 * time.Millisecond)

	// Connect to the server.
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

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

	// Stop the server.
	srv.Stop()
	wg.Wait()

	// Verify socket file is cleaned up.
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Errorf("socket file still exists after stop")
	}
}

func TestServer_Stop(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")

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
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

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
	dir := t.TempDir()
	// Unix sockets have a 108-char path limit. Use /tmp for the socket.
	sockDir, err := os.MkdirTemp("/tmp", "gh-daemon-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(sockDir)
	sock := filepath.Join(sockDir, "t.sock")
	cwd := dir

	// Build a minimal guard in yolo mode (auto-approve everything).
	wl := &guard.Whitelist{}
	dummyFe := &noopApprover{}
	g := guard.NewGuard(wl, dummyFe)
	g.SetYolo(true)

	registry := tools.NewRegistry()

	srv, err := NewServer(sock, ServerConfig{
		Version:      "test-agent",
		LLMClient:    &echoClient{},
		Guard:        g,
		Registry:     registry,
		SystemPrompt: "you are a test assistant",
		MaxTokens:    1024,
		Home:         dir,
		CWD:          cwd,
		SessionID:    "test-sess-1",
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Verify the agent was built.
	if srv.agent == nil {
		t.Fatal("expected agent to be non-nil after providing LLMClient")
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		srv.Serve()
	}()

	time.Sleep(50 * time.Millisecond)

	// Connect and send session.input to trigger the agent.
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send a session.input request.
	inputParams, _ := json.Marshal(rpc.SessionInputParams{Text: "hello"})
	msg := sendRequest(t, conn, 1, rpc.MethodSessionInput, inputParams)
	if msg.Error != nil {
		t.Fatalf("unexpected error on session.input: %v", msg.Error)
	}

	// The agent should process the input and emit events back. We read
	// notifications from the connection. Give the agent loop time to run.
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	rc := rpc.NewConn(conn)

	var notifications []*rpc.Message
	for {
		notif, err := rc.Read()
		if err != nil {
			break // timeout or connection closed
		}
		notifications = append(notifications, notif)
	}

	// We should have received at least one agent.event notification.
	if len(notifications) == 0 {
		t.Fatal("expected at least one agent.event notification, got none")
	}

	// Verify we got a token_delta event with "echo reply".
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
	sessionsDir := filepath.Join(dir, "sessions")
	var jsonlFiles []string
	filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
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
	defer f.Close()

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
	json.Unmarshal(events[0]["type"], &firstType)
	if firstType != "session_start" {
		t.Errorf("first JSONL event type = %q, want session_start", firstType)
	}

	// Clean up.
	srv.Stop()
	wg.Wait()
}

// noopApprover satisfies guard.Frontend for test purposes.
type noopApprover struct{}

func (n *noopApprover) RequestApproval(_ context.Context, _ guard.ApprovalRequest) (guard.ApprovalDecision, error) {
	return guard.ApprovalDecision{Outcome: guard.AllowOnce}, nil
}
