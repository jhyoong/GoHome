package main

import (
	"flag"
	"testing"
)

func TestIsDaemonRunning_NoSocket(t *testing.T) {
	// A non-existent socket should return false.
	if isDaemonRunning("/tmp/gohome-test-nonexistent.sock") {
		t.Error("isDaemonRunning returned true for non-existent socket")
	}
}

func TestConfigFlag_Defined(t *testing.T) {
	f := flag.Lookup("config")
	if f == nil {
		t.Fatal("expected --config flag to be defined")
	}
}
