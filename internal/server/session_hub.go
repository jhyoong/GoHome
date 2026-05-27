package server

import (
	"sync"

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
	req approval.Request
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
