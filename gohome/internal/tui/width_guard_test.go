package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
)

// finishes fails the test if fn does not return within a short time.
func finishes(t *testing.T, name string, fn func()) {
	t.Helper()
	done := make(chan any, 1)
	go func() {
		defer func() { done <- recover() }()
		fn()
	}()
	select {
	case r := <-done:
		if r != nil {
			t.Fatalf("%s: panic: %v", name, r)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("%s: did not finish (hang)", name)
	}
}

func TestWrapText_SmallWidthsFinish(t *testing.T) {
	inputs := map[string]string{
		"ascii": "hello world this is text",
		"cjk":   "你好世界你好世界",
		"mixed": "ab 你好 cd",
	}
	for name, in := range inputs {
		for _, w := range []int{-5, 0, 1, 2} {
			finishes(t, name, func() {
				got := WrapText(in, w)
				if len(got) == 0 {
					t.Errorf("%s width %d: no lines", name, w)
				}
			})
		}
	}
}

func TestView_TinyWidthAndNestedQuotesFinish(t *testing.T) {
	text := strings.Repeat("> ", 40) + "hello 你好 world\n\n---\n\n> > > > ---"
	for _, w := range []int{1, 2, 3, 80} {
		finishes(t, "view", func() {
			m := New(nil, "s1")
			m.Update(tea.WindowSizeMsg{Width: w, Height: 30})
			m.handleAgentEvent(AgentEventMsg{SessionID: "s1", Ev: agent.Event{
				Kind: agent.EventTokenDelta, TextDelta: text,
			}})
			m.handleAgentEvent(AgentEventMsg{SessionID: "s1", Ev: agent.Event{
				Kind: agent.EventTokenDelta, TextDelta: "\n",
			}})
			_ = m.View()
		})
	}
}

func TestRenderMarkdown_RuleSmallWidthNoPanic(t *testing.T) {
	for _, src := range []string{"---", "> ---", "> > > > ---", strings.Repeat("> ", 40) + "---"} {
		for _, w := range []int{-3, 0, 1, 2, 3, 5} {
			finishes(t, src, func() { _ = RenderMarkdown(src, w) })
		}
	}
}

func TestEditorRender_TinyWidthNoPanic(t *testing.T) {
	for _, w := range []int{-3, 0, 1, 2} {
		finishes(t, "editor", func() {
			e := NewEditor(w, 24)
			e.SetValue(strings.Repeat("line of text\n", 40))
			_ = e.Render(w)
		})
	}
}
