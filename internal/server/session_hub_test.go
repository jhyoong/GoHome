package server

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

func TestSessionHub_DispatchFansOutToAllWatchers(t *testing.T) {
	h := newTestHub(t)
	go h.Run() // start dispatch loop
	defer h.Stop()

	a := newFakeConn("tab-a")
	b := newFakeConn("tab-b")
	h.Watch(a)
	h.Watch(b)

	// Trigger a broker.Request in a goroutine; it will block on response.
	go func() {
		h.broker.Request(context.Background(), "req-1", "shell",
			json.RawMessage(`{"command":"ls"}`))
	}()

	// Both watchers must receive the approval prompt.
	for _, c := range []*wsConn{a, b} {
		select {
		case msg := <-c.outbound:
			if msg.Type != "tool_approval" || msg.RequestID != "req-1" {
				t.Fatalf("watcher %s got %+v", c.tabID, msg)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("watcher %s did not receive tool_approval", c.tabID)
		}
	}
}

func TestSessionHub_FirstResponderWins(t *testing.T) {
	h := newTestHub(t)
	go h.Run()
	defer h.Stop()

	a := newFakeConn("tab-a")
	b := newFakeConn("tab-b")
	h.Watch(a)
	h.Watch(b)

	resultCh := make(chan bool, 1)
	go func() {
		ok, _ := h.broker.Request(context.Background(), "req-1", "shell",
			json.RawMessage(`{"command":"ls"}`))
		resultCh <- ok
	}()

	// Wait until both tabs received the prompt before responding.
	<-a.outbound
	<-b.outbound

	var aWon, bWon atomic.Bool
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); aWon.Store(h.Respond("req-1", true)) }()
	go func() { defer wg.Done(); bWon.Store(h.Respond("req-1", false)) }()

	select {
	case approved := <-resultCh:
		wg.Wait()
		// Exactly one of aWon/bWon must be true; the broker result must
		// match the winner. Loser's vote is discarded.
		wins := 0
		if aWon.Load() {
			wins++
			if !approved {
				t.Fatal("tab-a won but broker returned false")
			}
		}
		if bWon.Load() {
			wins++
			if approved {
				t.Fatal("tab-b won but broker returned true")
			}
		}
		if wins != 1 {
			t.Fatalf("expected exactly 1 winner, got %d", wins)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("broker.Request never returned")
	}
}

func TestSessionHub_ResolvedEventBroadcast(t *testing.T) {
	h := newTestHub(t)
	go h.Run()
	defer h.Stop()

	a := newFakeConn("tab-a")
	b := newFakeConn("tab-b")
	h.Watch(a)
	h.Watch(b)

	go h.broker.Request(context.Background(), "req-1", "shell",
		json.RawMessage(`{"command":"ls"}`))
	<-a.outbound
	<-b.outbound

	h.Respond("req-1", true)

	// Both watchers must receive tool_approval_resolved.
	for _, c := range []*wsConn{a, b} {
		select {
		case msg := <-c.outbound:
			if msg.Type != "tool_approval_resolved" || msg.RequestID != "req-1" {
				t.Fatalf("watcher %s got %+v", c.tabID, msg)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("watcher %s did not receive resolved event", c.tabID)
		}
	}
}
