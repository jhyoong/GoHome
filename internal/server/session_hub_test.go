package server

import (
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
	if h.Idle() {
		// fresh hub with no watchers and zero refCount IS idle
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
	_ = approval.Broker{} // import sanity
}
