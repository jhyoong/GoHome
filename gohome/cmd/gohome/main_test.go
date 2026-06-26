package main

import (
	"testing"
)

func TestNewSessionID_Length(t *testing.T) {
	id := newSessionID()
	if len(id) != 8 {
		t.Errorf("newSessionID() = %q (len %d), want 8 chars", id, len(id))
	}
}

func TestNewSessionID_Unique(t *testing.T) {
	seen := make(map[string]bool)
	for range 100 {
		id := newSessionID()
		if seen[id] {
			t.Fatalf("duplicate session ID: %s", id)
		}
		seen[id] = true
	}
}

func TestIsDaemonRunning_NoSocket(t *testing.T) {
	// A non-existent socket should return false.
	if isDaemonRunning("/tmp/gohome-test-nonexistent.sock") {
		t.Error("isDaemonRunning returned true for non-existent socket")
	}
}
