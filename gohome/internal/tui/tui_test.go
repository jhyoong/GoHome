package tui_test

import (
	"bytes"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/tui"
)

func TestSkeletonRender(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() {
		_ = tm.Quit()
	})

	// The textarea placeholder must be visible on startup.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Type a message"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

func TestSessionViewTimelineRender(t *testing.T) {
	m := tui.New(nil, "")
	// Add a user entry to the focused "main" session.
	m.AddTimelineEntry("main", tui.TimelineEntry{Kind: "user", Text: "hello"})

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() {
		_ = tm.Quit()
	})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("hello"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

func TestAgentEventTokenDelta(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() {
		_ = tm.Quit()
	})

	tm.Send(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventTokenDelta,
		SessionID: "main",
		TextDelta: "hi",
	}})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("hi"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

func TestInputTextareaSubmit(t *testing.T) {
	fe := tui.NewFrontend()
	m := tui.New(fe, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() {
		_ = tm.Quit()
	})

	// Read the submitted text from the input channel in a goroutine.
	received := make(chan string, 1)
	go func() {
		select {
		case s := <-fe.InputCh():
			received <- s
		case <-time.After(3 * time.Second):
			received <- ""
		}
	}()

	// Type "world" then Enter.
	tm.Type("world")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert the input channel received the text.
	got := <-received
	if got != "world" {
		t.Fatalf("expected input channel to receive %q, got %q", "world", got)
	}

	// Assert the user entry appears in the rendered view.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("world"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

func TestViewportScrollback(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() {
		_ = tm.Quit()
	})

	// Send several agent token-delta events to populate the timeline.
	// Send PgUp/PgDown in between to verify scroll keys do not crash.
	entries := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	for _, text := range entries {
		tm.Send(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
			Kind:      agent.EventTokenDelta,
			SessionID: "main",
			TextDelta: text + "\n",
		}})
	}
	tm.Send(tea.KeyMsg{Type: tea.KeyPgUp})
	tm.Send(tea.KeyMsg{Type: tea.KeyPgDown})

	// The latest content should appear in the accumulated output.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("epsilon"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
