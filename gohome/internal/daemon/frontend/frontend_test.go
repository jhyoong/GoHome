package frontend_test

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/frontend"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
)

func setupTestFrontend(t *testing.T) (fe *frontend.RPCFrontend, tuiRPC *rpc.Conn, daemonRPC *rpc.Conn, cleanup func()) {
	t.Helper()
	daemonConn, tuiConn := net.Pipe()
	daemonRPC = rpc.NewConn(daemonConn)
	tuiRPC = rpc.NewConn(tuiConn)
	fe = frontend.New(daemonRPC)
	cleanup = func() {
		daemonConn.Close()
		tuiConn.Close()
	}
	return
}

func TestRPCFrontend_Emit(t *testing.T) {
	fe, tuiRPC, _, cleanup := setupTestFrontend(t)
	defer cleanup()

	ev := agent.Event{
		Kind:      agent.EventTokenDelta,
		SessionID: "s1",
		TextDelta: "hello",
	}

	// Emit in a goroutine because net.Pipe blocks writes until the reader
	// consumes the data.
	go fe.Emit("s1", ev)

	// Read the notification on the TUI side.
	msg, err := tuiRPC.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if !msg.IsNotification() {
		t.Fatalf("expected notification, got request/response")
	}
	if msg.Method != rpc.MethodAgentEvent {
		t.Fatalf("method = %q, want %q", msg.Method, rpc.MethodAgentEvent)
	}

	var params rpc.AgentEventParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params.SessionID != "s1" {
		t.Errorf("sessionID = %q, want %q", params.SessionID, "s1")
	}
	if params.Event.Kind != agent.EventTokenDelta {
		t.Errorf("event kind = %q, want %q", params.Event.Kind, agent.EventTokenDelta)
	}
	if params.Event.TextDelta != "hello" {
		t.Errorf("text delta = %q, want %q", params.Event.TextDelta, "hello")
	}
}

func TestRPCFrontend_RequestApproval(t *testing.T) {
	fe, tuiRPC, daemonRPC, cleanup := setupTestFrontend(t)
	defer cleanup()

	// Simulate the TUI side: read the approval request and send back AllowOnce.
	go func() {
		msg, err := tuiRPC.Read()
		if err != nil {
			t.Errorf("TUI read: %v", err)
			return
		}
		if !msg.IsRequest() {
			t.Errorf("expected request, got notification/response")
			return
		}
		if msg.Method != rpc.MethodApprovalRequest {
			t.Errorf("method = %q, want %q", msg.Method, rpc.MethodApprovalRequest)
			return
		}

		result := rpc.ApprovalResponseResult{
			Decision: guard.ApprovalDecision{
				Outcome: guard.AllowOnce,
			},
		}
		resultData, _ := json.Marshal(result)

		// Write the response back to the daemon side.
		err = tuiRPC.WriteResponse(rpc.Response{
			ID:     msg.ID,
			Result: resultData,
		})
		if err != nil {
			t.Errorf("TUI write response: %v", err)
		}
	}()

	// Simulate the daemon's read loop: read the response from the daemon
	// conn and deliver it to the pending tracker.
	go func() {
		msg, err := daemonRPC.Read()
		if err != nil {
			t.Errorf("daemon read loop: %v", err)
			return
		}
		if !msg.IsResponse() {
			t.Errorf("expected response, got request/notification")
			return
		}
		fe.ResolvePending(msg.ID.Int64(), msg.Result, msg.Error)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := guard.ApprovalRequest{
		SessionID: "s1",
		Tool:      "bash",
		Input:     json.RawMessage(`{"command":"ls"}`),
		Summary:   "run ls",
	}

	decision, err := fe.RequestApproval(ctx, req)
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if decision.Outcome != guard.AllowOnce {
		t.Errorf("outcome = %q, want %q", decision.Outcome, guard.AllowOnce)
	}
}

func TestRPCFrontend_RequestApproval_ContextCancelled(t *testing.T) {
	fe, tuiRPC, _, cleanup := setupTestFrontend(t)
	defer cleanup()

	// TUI side: read the request but never respond; just drain the message.
	go func() {
		_, _ = tuiRPC.Read()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req := guard.ApprovalRequest{
		SessionID: "s1",
		Tool:      "bash",
		Input:     json.RawMessage(`{"command":"rm -rf /"}`),
		Summary:   "dangerous command",
	}

	decision, err := fe.RequestApproval(ctx, req)
	// On context cancellation, the frontend should return Deny with an error
	// so the caller knows it was a cancellation, not a deliberate deny.
	if err == nil {
		t.Fatal("expected error on context cancellation, got nil")
	}
	if decision.Outcome != guard.Deny {
		t.Errorf("outcome = %q, want %q on ctx cancel", decision.Outcome, guard.Deny)
	}
}

func TestRPCFrontend_AwaitUserInput(t *testing.T) {
	fe, _, _, cleanup := setupTestFrontend(t)
	defer cleanup()

	// Deliver input from a goroutine after a short delay.
	go func() {
		time.Sleep(10 * time.Millisecond)
		if err := fe.DeliverInput("hello"); err != nil {
			t.Errorf("DeliverInput: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	text, err := fe.AwaitUserInput(ctx)
	if err != nil {
		t.Fatalf("AwaitUserInput: %v", err)
	}
	if text != "hello" {
		t.Errorf("text = %q, want %q", text, "hello")
	}
}

func TestRPCFrontend_AwaitUserInput_ContextCancelled(t *testing.T) {
	fe, _, _, cleanup := setupTestFrontend(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := fe.AwaitUserInput(ctx)
	if err == nil {
		t.Fatal("expected error on context cancellation, got nil")
	}
}
