package server

import (
	"encoding/json"
	"testing"

	"github.com/jhyoong/gohome/internal/approval"
	"github.com/jhyoong/gohome/internal/config"
)

func newTestHub(t *testing.T) *SessionHub {
	t.Helper()
	return NewSessionHub("sess-1", config.ApprovalConfig{DefaultTimeout: 5})
}

func TestSessionHub_RetainRelease(t *testing.T) {
	h := newTestHub(t)
	if !h.Idle() {
		t.Fatal("fresh hub must be idle")
	}
	h.Retain()
	if h.Idle() {
		t.Fatal("hub with refCount=1 must not be idle")
	}
	h.Release()
	if !h.Idle() {
		t.Fatal("hub with refCount=0 and no watchers/pending must be idle")
	}
}

func TestSessionHub_BrokerNotNil(t *testing.T) {
	h := newTestHub(t)
	if h.Broker() == nil {
		t.Fatal("Broker() must return non-nil")
	}
}

// fakeConn mirrors enough of *wsConn to capture fan-out messages.
// We send to its outbound channel directly. wsConn already exposes
// outbound for tests in send_test.go, so reusing it keeps the surface
// minimal.
func newFakeConn(tabID string) *wsConn {
	return &wsConn{
		tabID:    tabID,
		outbound: make(chan outMsg, 16),
	}
}

func TestSessionHub_WatchUnwatch(t *testing.T) {
	h := newTestHub(t)
	c := newFakeConn("tab-1")
	h.Watch(c)
	if h.Idle() {
		t.Fatal("hub with one watcher must not be idle")
	}
	h.Unwatch("tab-1")
	if !h.Idle() {
		t.Fatal("hub with zero watchers and zero refCount/pending must be idle")
	}
}

func TestSessionHub_WatchReplaysPending(t *testing.T) {
	h := newTestHub(t)
	// Inject a pending approval directly to test replay in isolation.
	h.mu.Lock()
	h.pending["req-1"] = &pendingApproval{req: approval.Request{
		ID: "req-1", Tool: "shell", Params: json.RawMessage(`{"command":"ls"}`),
	}}
	h.mu.Unlock()

	c := newFakeConn("tab-1")
	h.Watch(c)

	select {
	case msg := <-c.outbound:
		if msg.Type != "tool_approval" || msg.RequestID != "req-1" {
			t.Fatalf("expected replayed tool_approval, got %+v", msg)
		}
	default:
		t.Fatal("expected pending approval to be replayed on Watch")
	}
}

func TestSessionHub_WatchIdempotent(t *testing.T) {
	h := newTestHub(t)
	c := newFakeConn("tab-1")
	h.Watch(c)
	h.Watch(c) // second call must not duplicate
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.watchers) != 1 {
		t.Fatalf("expected 1 watcher after duplicate Watch, got %d", len(h.watchers))
	}
}
