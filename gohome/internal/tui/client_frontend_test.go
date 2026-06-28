package tui

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
)

type readResult struct {
	msg *rpc.Message
	err error
}

func setupClientFrontend(t *testing.T) (cf *ClientFrontend, daemonConn *rpc.Conn, events chan AgentEventMsg, cleanup func()) {
	t.Helper()
	daemonRaw, tuiRaw := net.Pipe()
	daemonConn = rpc.NewConn(daemonRaw)
	tuiConn := rpc.NewConn(tuiRaw)
	events = make(chan AgentEventMsg, 4)
	cf = NewClientFrontend(tuiConn, events)
	cleanup = func() {
		daemonRaw.Close()
		tuiRaw.Close()
	}
	return
}

func TestClientFrontend_ReceivesAgentEvent(t *testing.T) {
	cf, daemonConn, events, cleanup := setupClientFrontend(t)
	defer cleanup()

	// Run the read loop in a goroutine.
	go cf.ReadLoop()

	// Daemon sends an agent.event notification.
	params := rpc.AgentEventParams{
		SessionID: "s1",
		Event: agent.Event{
			Kind:      agent.EventTokenDelta,
			SessionID: "s1",
			TextDelta: "hello world",
		},
	}
	paramsData, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	err = daemonConn.Notify(rpc.MethodAgentEvent, paramsData)
	if err != nil {
		t.Fatalf("send notification: %v", err)
	}

	// Wait for the event to appear in the events channel.
	select {
	case ev := <-events:
		if ev.SessionID != "s1" {
			t.Errorf("sessionID = %q, want %q", ev.SessionID, "s1")
		}
		if ev.Ev.Kind != agent.EventTokenDelta {
			t.Errorf("event kind = %q, want %q", ev.Ev.Kind, agent.EventTokenDelta)
		}
		if ev.Ev.TextDelta != "hello world" {
			t.Errorf("text delta = %q, want %q", ev.Ev.TextDelta, "hello world")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for agent event")
	}
}

func TestClientFrontend_SendInput(t *testing.T) {
	cf, daemonConn, _, cleanup := setupClientFrontend(t)
	defer cleanup()

	// Start the read loop so we can handle responses (needed for pending calls).
	go cf.ReadLoop()

	// Daemon side: read the request and respond with success.
	ch := make(chan readResult, 1)
	go func() {
		msg, err := daemonConn.Read()
		ch <- readResult{msg, err}

		// Send a success response back so SendInput can return.
		if msg != nil && msg.ID != nil {
			_ = daemonConn.WriteResponse(rpc.Response{
				ID:     msg.ID,
				Result: json.RawMessage(`{}`),
			})
		}
	}()

	// Call SendInput.
	err := cf.SendInput("s1", "hello")
	if err != nil {
		t.Fatalf("SendInput: %v", err)
	}

	// Verify the request the daemon received.
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("daemon read: %v", r.err)
		}
		if !r.msg.IsRequest() {
			t.Fatal("expected request, got notification/response")
		}
		if r.msg.Method != rpc.MethodSessionInput {
			t.Errorf("method = %q, want %q", r.msg.Method, rpc.MethodSessionInput)
		}

		var params rpc.SessionInputParams
		if err := json.Unmarshal(r.msg.Params, &params); err != nil {
			t.Fatalf("unmarshal params: %v", err)
		}
		if params.SessionID != "s1" {
			t.Errorf("sessionID = %q, want %q", params.SessionID, "s1")
		}
		if params.Text != "hello" {
			t.Errorf("text = %q, want %q", params.Text, "hello")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for daemon to receive request")
	}
}

func TestClientFrontend_SendCancel(t *testing.T) {
	cf, daemonConn, _, cleanup := setupClientFrontend(t)
	defer cleanup()

	go cf.ReadLoop()

	ch := make(chan readResult, 1)
	go func() {
		msg, err := daemonConn.Read()
		ch <- readResult{msg, err}

		if msg != nil && msg.ID != nil {
			_ = daemonConn.WriteResponse(rpc.Response{
				ID:     msg.ID,
				Result: json.RawMessage(`{}`),
			})
		}
	}()

	err := cf.SendCancel("s1")
	if err != nil {
		t.Fatalf("SendCancel: %v", err)
	}

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("daemon read: %v", r.err)
		}
		if r.msg.Method != rpc.MethodSessionCancel {
			t.Errorf("method = %q, want %q", r.msg.Method, rpc.MethodSessionCancel)
		}

		var params rpc.SessionCancelParams
		if err := json.Unmarshal(r.msg.Params, &params); err != nil {
			t.Fatalf("unmarshal params: %v", err)
		}
		if params.SessionID != "s1" {
			t.Errorf("sessionID = %q, want %q", params.SessionID, "s1")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

func TestClientFrontend_RespondApproval(t *testing.T) {
	cf, daemonConn, _, cleanup := setupClientFrontend(t)
	defer cleanup()

	// Send a response for an approval.request from the daemon.
	reqID := rpc.NewID(42)
	dec := guard.ApprovalDecision{Outcome: guard.Deny}

	ch := make(chan readResult, 1)
	go func() {
		msg, err := daemonConn.Read()
		ch <- readResult{msg, err}
	}()

	err := cf.RespondApproval(reqID, dec)
	if err != nil {
		t.Fatalf("RespondApproval: %v", err)
	}

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("daemon read: %v", r.err)
		}
		if !r.msg.IsResponse() {
			t.Fatal("expected response, got request/notification")
		}
		if r.msg.ID.Int64() != 42 {
			t.Errorf("response id = %d, want 42", r.msg.ID.Int64())
		}

		var result rpc.ApprovalResponseResult
		if err := json.Unmarshal(r.msg.Result, &result); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		if result.Decision.Outcome != guard.Deny {
			t.Errorf("outcome = %q, want %q", result.Decision.Outcome, guard.Deny)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

func TestClientFrontend_ReceivesApprovalRequest(t *testing.T) {
	cf, daemonConn, _, cleanup := setupClientFrontend(t)
	defer cleanup()

	go cf.ReadLoop()

	// Daemon sends an approval.request.
	params := rpc.ApprovalRequestParams{
		SessionID:        "s1",
		Tool:             "bash",
		Input:            json.RawMessage(`{"command":"rm -rf /"}`),
		Summary:          "dangerous command",
		SuggestedPattern: "bash:rm*",
	}
	paramsData, _ := json.Marshal(params)
	err := daemonConn.WriteRequest(rpc.Request{
		ID:     rpc.NewID(99),
		Method: rpc.MethodApprovalRequest,
		Params: paramsData,
	})
	if err != nil {
		t.Fatalf("send approval request: %v", err)
	}

	// We expect an ApprovalReqMsg to appear in the events channel
	// (piggybacked as an AgentEventMsg with a special kind, or handled via
	// a separate mechanism). For now, the ReadLoop pushes an AgentEventMsg
	// wrapping an approval event. But the task says to push ApprovalReqMsg.
	// Since events is chan<- AgentEventMsg, the ReadLoop needs another
	// channel or we reuse the same channel. Let's check what comes through.
	//
	// Actually, looking at the requirements more carefully: approval.request
	// messages should create ApprovalReqMsg and push to events. Since our
	// events channel is chan<- AgentEventMsg, we need a way to carry
	// approval requests too. We'll use a separate approvals channel.

	// Wait for the approval to arrive.
	select {
	case req := <-cf.Approvals():
		if req.Req.SessionID != "s1" {
			t.Errorf("sessionID = %q, want %q", req.Req.SessionID, "s1")
		}
		if req.Req.Tool != "bash" {
			t.Errorf("tool = %q, want %q", req.Req.Tool, "bash")
		}
		if req.Resolve == nil {
			t.Fatal("expected Resolve callback to be set, got nil")
		}
		// Invoke the resolve callback and verify the daemon receives the response.
		respCh := make(chan *rpc.Message, 1)
		go func() {
			msg, err := daemonConn.Read()
			if err != nil {
				return
			}
			respCh <- msg
		}()
		req.Resolve(guard.ApprovalDecision{Outcome: guard.AllowOnce})
		select {
		case resp := <-respCh:
			if !resp.IsResponse() {
				t.Fatal("expected response from Resolve callback")
			}
			if resp.ID.Int64() != 99 {
				t.Errorf("response id = %d, want 99", resp.ID.Int64())
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for Resolve response")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for approval request")
	}
}

func TestClientFrontend_ReceivesSessionState(t *testing.T) {
	cf, daemonConn, _, cleanup := setupClientFrontend(t)
	defer cleanup()

	go cf.ReadLoop()

	// Daemon sends a session.state notification.
	params := rpc.SessionStateParams{
		SessionID: "sess-42",
		Model:     "claude-opus-4-20250514",
		Yolo:      true,
	}
	paramsData, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	err = daemonConn.Notify(rpc.MethodSessionState, paramsData)
	if err != nil {
		t.Fatalf("send notification: %v", err)
	}

	// Wait for the state sync message.
	select {
	case ss := <-cf.StateSync():
		if ss.SessionID != "sess-42" {
			t.Errorf("sessionID = %q, want %q", ss.SessionID, "sess-42")
		}
		if ss.Model != "claude-opus-4-20250514" {
			t.Errorf("model = %q, want %q", ss.Model, "claude-opus-4-20250514")
		}
		if !ss.Yolo {
			t.Error("yolo = false, want true")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for state sync message")
	}
}

func TestClientFrontend_Close(t *testing.T) {
	cf, _, _, cleanup := setupClientFrontend(t)
	defer cleanup()

	err := cf.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestClientFrontend_Close_ClosesChannels(t *testing.T) {
	server, client := net.Pipe()
	sc := rpc.NewConn(server)
	cc := rpc.NewConn(client)
	_ = sc

	eventCh := make(chan AgentEventMsg, 64)
	cfe := NewClientFrontend(cc, eventCh)
	go cfe.ReadLoop()

	_ = cfe.Close()
	sc.Close()

	// Approvals channel should be closed.
	_, ok := <-cfe.Approvals()
	if ok {
		t.Error("approvals channel should be closed after Close()")
	}
	// StateSync channel should be closed.
	_, ok = <-cfe.StateSync()
	if ok {
		t.Error("stateSync channel should be closed after Close()")
	}
}
