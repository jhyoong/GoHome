package tui_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/charmbracelet/x/exp/teatest"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/tui"
)

// TestStatusBarContent verifies the status bar renders the expected fields
// when the model has a focused session with non-zero usage, a modelName, and
// yolo mode enabled.
func TestStatusBarContent(t *testing.T) {
	m := tui.New(nil, "")
	m.SetModelName("opus")
	m.SetYolo(true)

	// Give the main session some usage so the bar has something to display.
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 24))
	t.Cleanup(func() {
		_ = tm.Quit()
	})

	// Send a usage-updated event to populate the token counts.
	tm.Send(tui.AgentEventMsg{
		SessionID: "main",
		Ev: agent.Event{
			Kind: agent.EventUsageUpdated,
			Usage: &common.Usage{
				InputTokens:  5000,
				OutputTokens: 3000,
			},
		},
	})

	// The rendered output must contain the key elements of the status bar.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return bytes.Contains(out, []byte("main")) &&
			bytes.Contains(out, []byte("opus")) &&
			bytes.Contains([]byte(s), []byte("%")) &&
			bytes.Contains(out, []byte("YOLO"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

// TestStatusBarNoYolo verifies [YOLO] is absent when yolo is false.
func TestStatusBarNoYolo(t *testing.T) {
	m := tui.New(nil, "")
	m.SetModelName("sonnet")
	// yolo defaults to false

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 24))
	t.Cleanup(func() {
		_ = tm.Quit()
	})

	// Wait until the model name appears in the output, confirming a render happened.
	var captured []byte
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		if bytes.Contains(out, []byte("sonnet")) {
			captured = out
			return true
		}
		return false
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	if bytes.Contains(captured, []byte("YOLO")) {
		t.Errorf("status bar should not contain YOLO when yolo=false, got: %s", captured)
	}
}

// TestStatusBarModelUnknown verifies "?" is shown when no modelName is set.
func TestStatusBarModelUnknown(t *testing.T) {
	m := tui.New(nil, "")
	// modelName defaults to ""

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 24))
	t.Cleanup(func() {
		_ = tm.Quit()
	})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("?"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
