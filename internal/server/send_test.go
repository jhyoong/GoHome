package server

import (
	"testing"
	"time"
)

// sendCritical must deliver subagent lifecycle events even under back-pressure.
// The legacy non-blocking send drops on a full channel; sendCritical waits up
// to a short timeout so that subagent_start / subagent_done / subagent_error
// are not silently lost (which on the frontend would leave the subagent
// invisible to the user).

func TestSendCritical_DeliversWhenSpaceAppearsBeforeTimeout(t *testing.T) {
	wc := &wsConn{outbound: make(chan outMsg, 1)}
	wc.outbound <- outMsg{Type: "filler"} // channel now full

	// Drain the filler 30ms in, freeing one slot
	go func() {
		time.Sleep(30 * time.Millisecond)
		<-wc.outbound
	}()

	start := time.Now()
	delivered := wc.sendCritical(outMsg{Type: "subagent_start"})
	elapsed := time.Since(start)

	if !delivered {
		t.Fatalf("expected delivery, got drop after %v", elapsed)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("delivery took %v, expected well under timeout", elapsed)
	}

	got := <-wc.outbound
	if got.Type != "subagent_start" {
		t.Errorf("delivered wrong message: %q", got.Type)
	}
}

func TestSendCritical_DropsAfterTimeoutWhenChannelStaysFull(t *testing.T) {
	wc := &wsConn{outbound: make(chan outMsg, 1)}
	wc.outbound <- outMsg{Type: "filler"} // never drained

	start := time.Now()
	delivered := wc.sendCritical(outMsg{Type: "subagent_done"})
	elapsed := time.Since(start)

	if delivered {
		t.Fatal("expected drop on persistently-full channel, got delivery")
	}
	if elapsed < 90*time.Millisecond {
		t.Errorf("sendCritical returned after %v, expected to wait ~100ms", elapsed)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("sendCritical waited %v, longer than expected", elapsed)
	}
}

func TestSendCritical_DeliversImmediatelyWhenChannelHasSpace(t *testing.T) {
	wc := &wsConn{outbound: make(chan outMsg, 4)}

	start := time.Now()
	delivered := wc.sendCritical(outMsg{Type: "subagent_error"})
	elapsed := time.Since(start)

	if !delivered {
		t.Fatal("expected immediate delivery into empty channel")
	}
	if elapsed > 10*time.Millisecond {
		t.Errorf("delivery took %v on empty channel, should be ~instant", elapsed)
	}

	if got := <-wc.outbound; got.Type != "subagent_error" {
		t.Errorf("got %q", got.Type)
	}
}
