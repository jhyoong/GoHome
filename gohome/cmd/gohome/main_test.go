package main

import (
	"testing"
)

func TestIsDaemonRunning_NoSocket(t *testing.T) {
	// A non-existent socket should return false.
	if isDaemonRunning("/tmp/gohome-test-nonexistent.sock") {
		t.Error("isDaemonRunning returned true for non-existent socket")
	}
}
