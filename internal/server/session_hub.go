package server

import (
	"sync"
	"sync/atomic"

	"github.com/jhyoong/gohome/internal/approval"
	"github.com/jhyoong/gohome/internal/config"
)

// SessionHub owns the approval.Broker for one session and fans approval
// requests out to all watcher connections (browser tabs) viewing that
// session. Created lazily when a session needs a broker; torn down when
// idle (no active agent runs, no watchers, no pending approvals).
type SessionHub struct {
	sessionID  string
	broker     *approval.Broker
	approvalCh chan approval.Request

	mu       sync.Mutex
	watchers map[string]*wsConn          // keyed by tabID
	pending  map[string]*pendingApproval // request_id → pending
	refCount int                         // active agent runs
}

type pendingApproval struct {
	req     approval.Request
	claimed atomic.Bool
}

// NewSessionHub constructs a hub for sessionID with its own approval Broker.
// The broker's outbound request channel is owned by the hub; do not close it
// externally. Call Run in a goroutine to start dispatching (Task 3).
func NewSessionHub(sessionID string, cfg config.ApprovalConfig) *SessionHub {
	ch := make(chan approval.Request, 8)
	return &SessionHub{
		sessionID:  sessionID,
		approvalCh: ch,
		broker:     approval.NewBroker(cfg, ch),
		watchers:   make(map[string]*wsConn),
		pending:    make(map[string]*pendingApproval),
	}
}

// Broker returns the approval broker bound to this hub. Pass it to the
// agent loop and any subagent spawn helpers for the session.
func (h *SessionHub) Broker() *approval.Broker { return h.broker }

func (h *SessionHub) Retain() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.refCount++
}

func (h *SessionHub) Release() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.refCount > 0 {
		h.refCount--
	}
}

// Idle returns true when the hub holds no active agent runs, no watchers,
// and no pending approvals. Caller should remove an idle hub.
func (h *SessionHub) Idle() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.refCount == 0 && len(h.watchers) == 0 && len(h.pending) == 0
}

// Watch registers a connection as an interested viewer of this session.
// Any pending approvals are replayed to the new watcher immediately so
// late-arriving tabs see the modal without waiting for a new request.
// Idempotent: calling Watch twice with the same tabID is a no-op for
// the second call.
func (h *SessionHub) Watch(wc *wsConn) {
	h.mu.Lock()
	h.watchers[wc.tabID] = wc
	// Snapshot pending for replay; release lock before sending to outbound
	// to avoid holding hub.mu during channel sends.
	replay := make([]approval.Request, 0, len(h.pending))
	for _, p := range h.pending {
		replay = append(replay, p.req)
	}
	h.mu.Unlock()

	for _, req := range replay {
		select {
		case wc.outbound <- outMsg{
			Type:      "tool_approval",
			RequestID: req.ID,
			Tool:      req.Tool,
			Params:    req.Params,
			SessionID: h.sessionID,
		}:
		default:
			// New tab with a full outbound is a bug elsewhere; log via send().
		}
	}
}

// Unwatch removes the tab from the watcher set. Safe to call for an
// unknown tabID (no-op).
func (h *SessionHub) Unwatch(tabID string) {
	h.mu.Lock()
	delete(h.watchers, tabID)
	h.mu.Unlock()
}

// Run is the dispatch loop. Call once in its own goroutine. It exits when
// Stop is called and the approval channel drains.
func (h *SessionHub) Run() {
	for req := range h.approvalCh {
		h.fanOut(req)
	}
}

// Stop closes the approval channel, causing Run to return after it
// drains. Call exactly once per hub; subsequent calls panic on
// double-close — caller is responsible (server.removeHubIfIdle uses
// sync.Map CompareAndDelete to enforce single ownership).
func (h *SessionHub) Stop() {
	close(h.approvalCh)
}

// fanOut sends the approval prompt to every current watcher and records
// the request in pending so late-joining watchers see it via replay.
func (h *SessionHub) fanOut(req approval.Request) {
	h.mu.Lock()
	h.pending[req.ID] = &pendingApproval{req: req}
	watchers := make([]*wsConn, 0, len(h.watchers))
	for _, w := range h.watchers {
		watchers = append(watchers, w)
	}
	h.mu.Unlock()

	msg := outMsg{
		Type:      "tool_approval",
		RequestID: req.ID,
		Tool:      req.Tool,
		Params:    req.Params,
		SessionID: h.sessionID,
	}
	for _, w := range watchers {
		select {
		case w.outbound <- msg:
		default:
			// Per design: do not block other watchers on one slow tab.
		}
	}
}

// Respond claims the pending approval atomically. Returns true if this
// caller won the claim (i.e. the broker was unblocked by this response).
// Subsequent calls for the same id return false. After winning, all
// watchers receive a tool_approval_resolved message so they dismiss
// their modal.
func (h *SessionHub) Respond(reqID string, approved bool, fromTabID string) bool {
	h.mu.Lock()
	pa, ok := h.pending[reqID]
	h.mu.Unlock()
	if !ok {
		return false
	}
	if !pa.claimed.CompareAndSwap(false, true) {
		return false
	}

	// Forward decision to broker (unblocks agent loop).
	h.broker.Respond(reqID, approved)

	// Remove from pending now that it's resolved.
	h.mu.Lock()
	delete(h.pending, reqID)
	watchers := make([]*wsConn, 0, len(h.watchers))
	for _, w := range h.watchers {
		watchers = append(watchers, w)
	}
	h.mu.Unlock()

	// Tell all watchers (including the one that responded) to dismiss.
	resolved := outMsg{
		Type:      "tool_approval_resolved",
		RequestID: reqID,
		SessionID: h.sessionID,
	}
	for _, w := range watchers {
		select {
		case w.outbound <- resolved:
		default:
		}
	}
	return true
}
