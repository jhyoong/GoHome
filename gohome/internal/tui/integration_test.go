package tui_test

import (
	"bytes"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
	"github.com/jhyoong/GoHome/gohome/internal/tui"
)

func TestSlashResumeOpensAndCloses(t *testing.T) {
	m := tui.New(nil, "")
	m.SetSlashCallbacks(tui.SlashCallbacks{
		ListSessions: func() ([]session.Listing, error) {
			return []session.Listing{
				{ID: "s1", Title: "test session"},
			}, nil
		},
	})

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Type("/resume")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("test session"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

func TestSlashResumeWithFilterPreFills(t *testing.T) {
	m := tui.New(nil, "")
	m.SetSlashCallbacks(tui.SlashCallbacks{
		ListSessions: func() ([]session.Listing, error) {
			return []session.Listing{
				{ID: "s1", Title: "fix login bug"},
				{ID: "s2", Title: "refactor renderer"},
			}, nil
		},
	})

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Type("/resume login")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("fix login bug"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

func TestSlashResumeLoadsHistory(t *testing.T) {
	m := tui.New(nil, "")
	m.SetSlashCallbacks(tui.SlashCallbacks{
		ListSessions: func() ([]session.Listing, error) {
			return []session.Listing{
				{ID: "s1", Title: "test session"},
			}, nil
		},
		ResumeSession: func(id string) ([]common.Message, error) {
			return []common.Message{
				{Role: common.RoleUser, Content: []common.Block{
					{Kind: common.BlockText, Text: "previous question"},
				}},
				{Role: common.RoleAssistant, Content: []common.Block{
					{Kind: common.BlockText, Text: "previous answer"},
				}},
			}, nil
		},
	})

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Type("/resume")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("test session"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Select the session (Enter on the first item).
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Historical messages should appear in the rendered output.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("previous question")) &&
			bytes.Contains(out, []byte("previous answer"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

func TestSlashModelOpensSelector(t *testing.T) {
	m := tui.New(nil, "")
	m.SetSettings(config.Settings{
		Endpoints: map[string]config.Endpoint{
			"anthropic": {DefaultModel: "claude-sonnet-4-20250514"},
			"openai":    {DefaultModel: "gpt-4o"},
		},
		DefaultEndpoint: "anthropic",
	})

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Type("/model")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("anthropic"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
