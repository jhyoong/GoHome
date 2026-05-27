package server

import (
	"testing"

	"github.com/jhyoong/gohome/internal/config"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	return New(Config{Approval: config.ApprovalConfig{DefaultTimeout: 5}})
}

func TestServer_GetOrCreateHubReturnsSameInstance(t *testing.T) {
	s := newTestServer(t)
	h1 := s.getOrCreateHub("sess-1")
	h2 := s.getOrCreateHub("sess-1")
	if h1 != h2 {
		t.Fatal("getOrCreateHub must return same hub for same session")
	}
	h3 := s.getOrCreateHub("sess-2")
	if h1 == h3 {
		t.Fatal("getOrCreateHub must return different hubs for different sessions")
	}
}

func TestServer_RemoveHubIfIdle(t *testing.T) {
	s := newTestServer(t)
	h := s.getOrCreateHub("sess-1")
	if !h.Idle() {
		t.Fatal("fresh hub must be idle")
	}
	s.removeHubIfIdle("sess-1")
	// A fresh getOrCreateHub after removal must create a new instance.
	h2 := s.getOrCreateHub("sess-1")
	if h == h2 {
		t.Fatal("expected new hub after removal of idle hub")
	}
}

func TestServer_RemoveHubIfIdleSkipsNonIdle(t *testing.T) {
	s := newTestServer(t)
	h := s.getOrCreateHub("sess-1")
	h.Retain()
	s.removeHubIfIdle("sess-1")
	if h2 := s.getOrCreateHub("sess-1"); h2 != h {
		t.Fatal("non-idle hub must not be removed")
	}
}
