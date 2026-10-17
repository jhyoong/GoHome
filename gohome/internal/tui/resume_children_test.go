package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

// resumedParentHistory is a parent history with two sequential subagent calls.
func resumedParentHistory() []common.Message {
	subagentTurn := func(id, task string) []common.Message {
		return []common.Message{
			{Role: common.RoleAssistant, Content: []common.Block{
				{Kind: common.BlockToolUse, ToolUseID: id, ToolName: "subagent", InputJSON: `{"task":"` + task + `"}`},
			}},
			{Role: common.RoleTool, Content: []common.Block{
				{Kind: common.BlockToolResult, ToolUseID: id, ResultText: task + " done"},
			}},
		}
	}
	h := []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "do two things"}}},
	}
	h = append(h, subagentTurn("tu1", "first")...)
	h = append(h, subagentTurn("tu2", "second")...)
	return h
}

func resumedChild(id, task string) *session.Session {
	c := session.NewSession(id, "/tmp", "m", "e")
	c.Depth = 1
	c.ParentID = "p1"
	c.History = []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: task}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: task + " answer"}}},
	}
	return c
}

func resumedChildren() []*session.Session {
	return []*session.Session{resumedChild("p1-sub-1", "first"), resumedChild("p1-sub-2", "second")}
}

// assertResumedChildren checks that both children have read-only views
// linked, in order, to the parent's subagent tool entries.
func assertResumedChildren(t *testing.T, m *Model, parentID string) {
	t.Helper()
	ps := m.sessions[parentID]
	if ps == nil {
		t.Fatalf("no view for parent %q", parentID)
	}
	var linked []string
	for _, e := range ps.Timeline {
		if e.Kind == KindTool && e.ToolName == "subagent" {
			linked = append(linked, e.ChildSessionID)
		}
	}
	if len(linked) != 2 || linked[0] != "p1-sub-1" || linked[1] != "p1-sub-2" {
		t.Fatalf("subagent entries linked to %v, want [p1-sub-1 p1-sub-2]", linked)
	}
	for _, id := range []string{"p1-sub-1", "p1-sub-2"} {
		sv := m.sessions[id]
		if sv == nil {
			t.Fatalf("no view for child %q; order = %v", id, m.order)
		}
		if sv.Depth != 1 || !sv.Completed || sv.InFlight {
			t.Errorf("child %q: Depth=%d Completed=%v InFlight=%v, want 1/true/false", id, sv.Depth, sv.Completed, sv.InFlight)
		}
		if len(sv.Timeline) != 2 {
			t.Errorf("child %q timeline len = %d, want 2", id, len(sv.Timeline))
		}
		if m.childToParent[id] != parentID {
			t.Errorf("childToParent[%q] = %q, want %q", id, m.childToParent[id], parentID)
		}
	}
}

func TestLoadResumedSession_LinksChildren(t *testing.T) {
	m := New(nil, "p1")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m.LoadResumedSession("p1", resumedParentHistory(), resumedChildren())

	assertResumedChildren(t, m, "p1")
	if m.focused != "p1" {
		t.Errorf("focused = %q, want p1", m.focused)
	}
}

func TestLoadResumedSession_ChildIsReadOnly(t *testing.T) {
	m := New(nil, "p1")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.LoadResumedSession("p1", resumedParentHistory(), resumedChildren())

	// Focus the first child and try to send a message.
	pressKey(m, tea.KeyCtrlRight)
	if m.focused != "p1-sub-1" {
		t.Fatalf("focused = %q, want p1-sub-1", m.focused)
	}
	typeRunes(m, "hello child")
	pressKey(m, tea.KeyEnter)

	sv := m.sessions["p1-sub-1"]
	if len(sv.Timeline) != 2 || sv.InFlight {
		t.Errorf("read-only child accepted input: timeline len %d, InFlight %v", len(sv.Timeline), sv.InFlight)
	}
	select {
	case got := <-m.inputCh:
		t.Errorf("input sent to agent from read-only child: %q", got)
	default:
	}
}

func TestSlashResumeLoadsChildren(t *testing.T) {
	m := New(nil, "")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.SetSlashCallbacks(SlashCallbacks{
		ListSessions: func() ([]session.Listing, error) {
			return []session.Listing{{ID: "p1", Title: "parent"}}, nil
		},
		ResumeSession: func(id string) (string, []common.Message, []*session.Session, error) {
			return id, resumedParentHistory(), resumedChildren(), nil
		},
	})

	typeRunes(m, "/resume")
	pressKey(m, tea.KeyEnter)
	pressKey(m, tea.KeyEnter) // select the only session

	if m.focused != "p1" {
		t.Fatalf("focused = %q, want p1", m.focused)
	}
	assertResumedChildren(t, m, "p1")
}

// subagentTurn is one subagent tool call and its result in a parent history.
func subagentTurn(id, task, result string, isErr bool) []common.Message {
	return []common.Message{
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockToolUse, ToolUseID: id, ToolName: "subagent", InputJSON: `{"task":"` + task + `"}`},
		}},
		{Role: common.RoleTool, Content: []common.Block{
			{Kind: common.BlockToolResult, ToolUseID: id, ResultText: result, IsError: isErr},
		}},
	}
}

// subagentLinks returns ChildSessionID for each subagent entry, keyed by ToolUseID.
func subagentLinks(m *Model, parentID string) map[string]string {
	links := map[string]string{}
	for _, e := range m.sessions[parentID].Timeline {
		if e.Kind == KindTool && e.ToolName == "subagent" {
			links[e.ToolUseID] = e.ChildSessionID
		}
	}
	return links
}

func TestLoadResumedSession_DeniedEntryNotLinked(t *testing.T) {
	m := New(nil, "p1")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	h := []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "go"}}},
	}
	h = append(h, subagentTurn("tu0", "denied task", "denied by user", true)...)
	h = append(h, subagentTurn("tu1", "first", "first done", false)...)
	h = append(h, subagentTurn("tu2", "second", "second done", false)...)

	m.LoadResumedSession("p1", h, resumedChildren())

	got := subagentLinks(m, "p1")
	want := map[string]string{"tu0": "", "tu1": "p1-sub-1", "tu2": "p1-sub-2"}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("entry %s linked to %q, want %q (all: %v)", id, got[id], w, got)
		}
	}
}

func TestLoadResumedSession_CompactedParent(t *testing.T) {
	m := New(nil, "p1")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	// Compaction removed the entries for the first two subagent calls.
	h := []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "summary of earlier work"}}},
	}
	h = append(h, subagentTurn("tu3", "third", "third done", false)...)
	children := []*session.Session{
		resumedChild("p1-sub-1", "first"),
		resumedChild("p1-sub-2", "second"),
		resumedChild("p1-sub-3", "third"),
	}

	m.LoadResumedSession("p1", h, children)

	if got := subagentLinks(m, "p1")["tu3"]; got != "p1-sub-3" {
		t.Errorf("tu3 linked to %q, want p1-sub-3", got)
	}
	for _, c := range children {
		if sv := m.sessions[c.ID]; sv == nil || !sv.Completed {
			t.Errorf("child %q has no read-only view", c.ID)
		}
	}
}

func TestLoadResumedSession_ChildWithoutEntry(t *testing.T) {
	m := New(nil, "p1")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	h := []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "go"}}},
	}
	h = append(h, subagentTurn("tu1", "first", "first done", false)...)
	children := []*session.Session{
		resumedChild("p1-sub-1", "first"),
		resumedChild("p1-sub-2", "orphan"),
	}

	m.LoadResumedSession("p1", h, children)

	if got := subagentLinks(m, "p1")["tu1"]; got != "p1-sub-1" {
		t.Errorf("tu1 linked to %q, want p1-sub-1", got)
	}
	sv := m.sessions["p1-sub-2"]
	if sv == nil {
		t.Fatalf("no view for unlinked child p1-sub-2")
	}
	if !sv.Completed || sv.InFlight {
		t.Errorf("unlinked child: Completed=%v InFlight=%v, want true/false", sv.Completed, sv.InFlight)
	}
	if p, ok := m.childToParent["p1-sub-2"]; ok {
		t.Errorf("unlinked child has parent %q, want none", p)
	}
}
