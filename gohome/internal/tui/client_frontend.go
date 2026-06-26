package tui

import (
	"context"
	"encoding/json"
	"sync/atomic"

	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

// ClientApprovalReqMsg wraps an incoming approval.request from the daemon.
// It extends ApprovalReqMsg with the JSON-RPC ID needed to send the response
// back. The Reply channel is nil because the daemon drives the reply flow via
// RespondApproval.
type ClientApprovalReqMsg struct {
	Req   guard.ApprovalRequest
	RPCID *rpc.ID
}

// ClientFrontend connects the TUI to a daemon over JSON-RPC. It receives
// agent.event notifications and converts them to Bubble Tea messages, and
// sends user input and approval decisions as JSON-RPC requests.
type ClientFrontend struct {
	conn      *rpc.Conn
	events    chan<- AgentEventMsg
	approvals chan ClientApprovalReqMsg
	pending   *rpc.Pending
	idSeq     atomic.Int64
}

// NewClientFrontend creates a ClientFrontend wired to the given connection.
// Agent events are pushed to the events channel. Approval requests from the
// daemon are pushed to a separate approvals channel accessible via Approvals().
func NewClientFrontend(conn *rpc.Conn, events chan<- AgentEventMsg) *ClientFrontend {
	return &ClientFrontend{
		conn:      conn,
		events:    events,
		approvals: make(chan ClientApprovalReqMsg, 4),
		pending:   rpc.NewPending(),
	}
}

// Approvals returns a read-only channel that delivers approval requests from
// the daemon. The TUI reads from this channel and eventually calls
// RespondApproval with the decision.
func (cf *ClientFrontend) Approvals() <-chan ClientApprovalReqMsg {
	return cf.approvals
}

// ReadLoop reads messages from the daemon in a loop. It should be run in a
// goroutine. It returns when the connection is closed or an unrecoverable
// read error occurs.
func (cf *ClientFrontend) ReadLoop() {
	for {
		msg, err := cf.conn.Read()
		if err != nil {
			return
		}

		switch {
		case msg.IsResponse():
			// Forward to pending tracker so blocked Send* calls can complete.
			if msg.ID != nil {
				cf.pending.Resolve(msg.ID.Int64(), msg.Result, msg.Error)
			}

		case msg.IsNotification():
			cf.handleNotification(msg)

		case msg.IsRequest():
			cf.handleRequest(msg)
		}
	}
}

// handleNotification processes incoming notifications from the daemon.
func (cf *ClientFrontend) handleNotification(msg *rpc.Message) {
	switch msg.Method {
	case rpc.MethodAgentEvent:
		var params rpc.AgentEventParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return
		}
		cf.events <- AgentEventMsg{
			SessionID: params.SessionID,
			Ev:        params.Event,
		}

	case rpc.MethodSessionState:
		// Ignored for now; will be handled in Task 15.
	}
}

// handleRequest processes incoming requests from the daemon (e.g. approval.request).
func (cf *ClientFrontend) handleRequest(msg *rpc.Message) {
	switch msg.Method {
	case rpc.MethodApprovalRequest:
		var params rpc.ApprovalRequestParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return
		}
		cf.approvals <- ClientApprovalReqMsg{
			Req: guard.ApprovalRequest{
				SessionID:        params.SessionID,
				Tool:             params.Tool,
				Input:            params.Input,
				Summary:          params.Summary,
				SuggestedPattern: params.SuggestedPattern,
			},
			RPCID: msg.ID,
		}
	}
}

// sendRequest is a helper that marshals params, writes a JSON-RPC request, and
// blocks until the daemon responds via the pending tracker.
func (cf *ClientFrontend) sendRequest(method string, params any) error {
	id := cf.idSeq.Add(1)

	paramsData, err := json.Marshal(params)
	if err != nil {
		return err
	}

	err = cf.conn.WriteRequest(rpc.Request{
		ID:     rpc.NewID(id),
		Method: method,
		Params: paramsData,
	})
	if err != nil {
		return err
	}

	// Block until the response arrives. Use a background context; callers
	// that need cancellation can use the context-aware variant later.
	_, err = cf.pending.Call(context.Background(), id)
	return err
}

// SendInput sends user text input to the daemon for the given session.
func (cf *ClientFrontend) SendInput(sessionID, text string) error {
	return cf.sendRequest(rpc.MethodSessionInput, rpc.SessionInputParams{
		SessionID: sessionID,
		Text:      text,
	})
}

// SendApproval sends a user approval decision to the daemon for the given session.
func (cf *ClientFrontend) SendApproval(sessionID string, dec guard.ApprovalDecision) error {
	return cf.sendRequest(rpc.MethodSessionApproval, rpc.SessionApprovalParams{
		SessionID: sessionID,
		Decision:  dec,
	})
}

// SendCancel sends a cancel request to the daemon for the given session.
func (cf *ClientFrontend) SendCancel(sessionID string) error {
	return cf.sendRequest(rpc.MethodSessionCancel, rpc.SessionCancelParams{
		SessionID: sessionID,
	})
}

// RespondApproval sends a JSON-RPC response back to the daemon for a pending
// approval.request. This is the TUI's answer to the daemon's question about
// whether a tool call should be permitted.
func (cf *ClientFrontend) RespondApproval(id *rpc.ID, dec guard.ApprovalDecision) error {
	result := rpc.ApprovalResponseResult{Decision: dec}
	resultData, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return cf.conn.WriteResponse(rpc.Response{
		ID:     id,
		Result: resultData,
	})
}

// SendSessionList sends a session.list request and returns the listings.
func (cf *ClientFrontend) SendSessionList() ([]session.Listing, error) {
	raw, err := cf.sendRequestWithResult(rpc.MethodSessionList, json.RawMessage(`{}`))
	if err != nil {
		return nil, err
	}
	var result rpc.SessionListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result.Sessions, nil
}

// SendSessionNew sends a session.new request and returns the new session ID.
func (cf *ClientFrontend) SendSessionNew() (string, error) {
	raw, err := cf.sendRequestWithResult(rpc.MethodSessionNew, json.RawMessage(`{}`))
	if err != nil {
		return "", err
	}
	var result rpc.SessionNewResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	return result.SessionID, nil
}

// SendSessionResume sends a session.resume request and returns the session ID and history.
func (cf *ClientFrontend) SendSessionResume(id string) (string, []common.Message, error) {
	raw, err := cf.sendRequestWithResult(rpc.MethodSessionResume, rpc.SessionResumeParams{ID: id})
	if err != nil {
		return "", nil, err
	}
	var result rpc.SessionResumeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", nil, err
	}
	var history []common.Message
	if err := json.Unmarshal(result.History, &history); err != nil {
		return result.SessionID, nil, err
	}
	return result.SessionID, history, nil
}

// SendModelSet sends a model.set request and returns the model name and context window.
func (cf *ClientFrontend) SendModelSet(name string) (string, int, error) {
	raw, err := cf.sendRequestWithResult(rpc.MethodModelSet, rpc.ModelSetParams{Name: name})
	if err != nil {
		return "", 0, err
	}
	var result rpc.ModelSetResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", 0, err
	}
	return result.ModelName, result.ContextWindow, nil
}

// sendRequestWithResult is like sendRequest but returns the raw result data.
func (cf *ClientFrontend) sendRequestWithResult(method string, params any) (json.RawMessage, error) {
	id := cf.idSeq.Add(1)

	paramsData, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	err = cf.conn.WriteRequest(rpc.Request{
		ID:     rpc.NewID(id),
		Method: method,
		Params: paramsData,
	})
	if err != nil {
		return nil, err
	}

	return cf.pending.Call(context.Background(), id)
}

// Close closes the underlying connection.
func (cf *ClientFrontend) Close() error {
	return cf.conn.Close()
}
