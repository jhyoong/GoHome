// Package frontend provides RPCFrontend, the daemon-side implementation of
// agent.Frontend that communicates with a connected TUI client over JSON-RPC.
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

// Compile-time check that RPCFrontend implements agent.Frontend.
var _ agent.Frontend = (*RPCFrontend)(nil)

// RPCFrontend implements agent.Frontend by serialising events and approval
// requests over a JSON-RPC Conn. The daemon creates one RPCFrontend per
// connected TUI client.
type RPCFrontend struct {
	conn    *rpc.Conn
	pending *rpc.Pending
	idSeq   atomic.Int64
	inputCh chan string
	done    chan struct{}
}

// New creates an RPCFrontend that communicates over conn.
func New(conn *rpc.Conn) *RPCFrontend {
	return &RPCFrontend{
		conn:    conn,
		pending: rpc.NewPending(),
		inputCh: make(chan string, 1),
		done:    make(chan struct{}),
	}
}

// Close signals that this frontend is no longer active. Any blocked
// AwaitUserInput call will return with an error.
func (f *RPCFrontend) Close() {
	select {
	case <-f.done:
	default:
		close(f.done)
	}
}

// Emit sends an agent.event JSON-RPC notification to the TUI. Errors are
// silently dropped because the agent must not block on a broken connection.
func (f *RPCFrontend) Emit(sessionID string, ev agent.Event) {
	params := rpc.AgentEventParams{
		SessionID: sessionID,
		Event:     ev,
	}
	data, err := json.Marshal(params)
	if err != nil {
		return
	}
	_ = f.conn.Notify(rpc.MethodAgentEvent, data)
}

// RequestApproval sends an approval.request JSON-RPC request to the TUI and
// blocks until the TUI responds or ctx is cancelled. On any error it returns
// a Deny decision.
func (f *RPCFrontend) RequestApproval(ctx context.Context, req guard.ApprovalRequest) (guard.ApprovalDecision, error) {
	deny := guard.ApprovalDecision{Outcome: guard.Deny}

	id := f.idSeq.Add(1)

	params := rpc.ApprovalRequestParams{
		SessionID:        req.SessionID,
		Tool:             req.Tool,
		Input:            req.Input,
		Summary:          req.Summary,
		SuggestedPattern: req.SuggestedPattern,
	}
	paramsData, err := json.Marshal(params)
	if err != nil {
		return guard.ApprovalDecision{}, fmt.Errorf("marshal approval params: %w", err)
	}

	f.pending.Register(id)

	err = f.conn.WriteRequest(rpc.Request{
		ID:     rpc.NewID(id),
		Method: rpc.MethodApprovalRequest,
		Params: paramsData,
	})
	if err != nil {
		f.pending.Cancel(id)
		return guard.ApprovalDecision{}, fmt.Errorf("write approval request: %w", err)
	}

	raw, err := f.pending.Wait(ctx, id)
	if err != nil {
		return deny, fmt.Errorf("wait for approval response: %w", err)
	}

	var result rpc.ApprovalResponseResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return guard.ApprovalDecision{}, fmt.Errorf("unmarshal approval response: %w", err)
	}

	return result.Decision, nil
}

// AwaitUserInput blocks until DeliverInput is called, ctx is cancelled, or the
// frontend is closed.
func (f *RPCFrontend) AwaitUserInput(ctx context.Context, _ string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-f.done:
		return "", fmt.Errorf("frontend closed")
	case text := <-f.inputCh:
		return text, nil
	}
}

// DeliverInput pushes user input text into the frontend. It is called by the
// daemon server when it receives a session.input request from the TUI.
// It returns an error if the input channel is full (agent is not awaiting input).
func (f *RPCFrontend) DeliverInput(text string) error {
	select {
	case f.inputCh <- text:
		return nil
	default:
		return fmt.Errorf("input channel full: agent is not awaiting input")
	}
}

// ResolvePending delivers a JSON-RPC response to a waiting RequestApproval
// call. It is called by the daemon's read loop when a response arrives for an
// in-flight request.
func (f *RPCFrontend) ResolvePending(id int64, result json.RawMessage, rpcErr *rpc.Error) {
	f.pending.Resolve(id, result, rpcErr)
}
