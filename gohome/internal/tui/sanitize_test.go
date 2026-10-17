package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

func TestSanitizeText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello world", "hello world"},
		{"newline and tab kept", "a\n\tb", "a\n\tb"},
		{"unicode kept", "café 世界", "café 世界"},
		{"OSC 52 with ST", "a\x1b]52;c;aGVsbG8=\x1b\\b", "ab"},
		{"OSC with BEL", "a\x1b]0;title\x07b", "ab"},
		{"CSI clear screen", "a\x1b[2Jb", "ab"},
		{"CSI cursor home", "a\x1b[Hb", "ab"},
		{"SGR color", "\x1b[31mred\x1b[0m", "red"},
		{"CSI private mode", "a\x1b[?1049hb", "ab"},
		{"DCS", "a\x1bPq#0;2;0;0;0\x1b\\b", "ab"},
		{"SOS", "a\x1bXhidden\x1b\\b", "ab"},
		{"PM", "a\x1b^hidden\x1b\\b", "ab"},
		{"APC", "a\x1b_hidden\x1b\\b", "ab"},
		{"two-byte ESC", "a\x1bcb", "ab"},
		{"charset designation", "a\x1b(Bb", "ab"},
		{"DECALN", "a\x1b#8b", "ab"},
		{"C1 CSI", "a\u009b2Jb", "ab"},
		{"C1 OSC with C1 ST", "a\u009d52;c;aGk=\u009cb", "ab"},
		{"C1 OSC with BEL", "a\u009d0;t\x07b", "ab"},
		{"C1 single", "a\u0085b", "ab"},
		{"C0 controls", "a\x00\x07\x08\x0b\x0cb", "ab"},
		{"DEL", "a\x7fb", "ab"},
		{"CRLF", "a\r\nb", "a\nb"},
		{"lone CR", "a\rb", "ab"},
		{"unterminated OSC", "a\x1b]52;c;aGk=", "a"},
		{"unterminated CSI", "a\x1b[2", "a"},
		{"trailing ESC", "a\x1b", "a"},
		{"invalid UTF-8", "a\x9bb", "ab"},
		{"ESC inside OSC ends it", "a\x1b]0;t\x1b[2Jb", "ab"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeText(tc.in); got != tc.want {
				t.Errorf("sanitizeText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizeStream_HoldsIncompleteTail(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantClean string
		wantRest  string
	}{
		{"complete", "ab\x1b[2Jc", "abc", ""},
		{"split OSC", "ab\x1b]52;c;aG", "ab", "\x1b]52;c;aG"},
		{"split CSI", "ab\x1b[2", "ab", "\x1b[2"},
		{"trailing ESC", "ab\x1b", "ab", "\x1b"},
		{"split rune", "ab\xe4\xb8", "ab", "\xe4\xb8"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clean, rest := sanitizeStream(tc.in)
			if clean != tc.wantClean || rest != tc.wantRest {
				t.Errorf("sanitizeStream(%q) = (%q, %q), want (%q, %q)",
					tc.in, clean, rest, tc.wantClean, tc.wantRest)
			}
		})
	}
}

// hostile holds sequences that must never reach the terminal.
var hostile = []string{
	"\x1b]52;c;aGVsbG8=\x07", // OSC 52 clipboard write, BEL-terminated
	"\x1b]52;c;aGk=\x1b\\",   // OSC 52 with ST
	"\x1b[2J",                // clear screen
	"\x1b[35m",               // color (the app itself never uses magenta)
}

// assertNoHostile fails if the rendered view contains any of the hostile
// sequences. The app adds its own SGR styling (dim, italic, reset), so the
// check looks for the specific sequences instead of any ESC.
func assertNoHostile(t *testing.T, view string) {
	t.Helper()
	for _, bad := range []string{"\x1b]", "\x07", "\x1b[2J", "\x1b[35m", "52;c;"} {
		if strings.Contains(view, bad) {
			t.Errorf("view contains %q: %q", bad, view)
		}
	}
}

func newSanitizeModel() *Model {
	m := New(nil, "s1")
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 60})
	return m
}

func TestView_SanitizesAgentEvents(t *testing.T) {
	payload := "before" + strings.Join(hostile, "") + "after\tTAB\nnext line"

	m := newSanitizeModel()
	m.handleAgentEvent(AgentEventMsg{SessionID: "s1", Ev: agent.Event{
		Kind: agent.EventThinkingDelta, ThinkingDelta: payload,
	}})
	m.handleAgentEvent(AgentEventMsg{SessionID: "s1", Ev: agent.Event{
		Kind: agent.EventTokenDelta, TextDelta: payload,
	}})
	m.handleAgentEvent(AgentEventMsg{SessionID: "s1", Ev: agent.Event{
		Kind: agent.EventToolCallDone, ToolCallID: "t1", ToolName: "shell",
		// JSON escapes decode to real control characters in the summary line.
		InputJSON: `{"command":"echo \u001b]0;t\u0007 \u001b[2J"}`,
	}})
	m.handleAgentEvent(AgentEventMsg{SessionID: "s1", Ev: agent.Event{
		Kind: agent.EventToolCallDone, ToolCallID: "t2", ToolName: "shell",
		// Raw control characters make the input invalid JSON.
		InputJSON: "{\"command\":\"echo \x1b]0;t\x07 \x1b[2J\"}",
	}})
	m.handleAgentEvent(AgentEventMsg{SessionID: "s1", Ev: agent.Event{
		Kind:   agent.EventToolResult,
		Result: &agent.ToolResult{ToolUseID: "t1", Content: payload},
	}})
	m.handleAgentEvent(AgentEventMsg{SessionID: "s1", Ev: agent.Event{
		Kind: agent.EventError, Err: errString(payload),
	}})

	assertNoHostile(t, m.View())
	sv := m.sessions["s1"]
	for i := range sv.Timeline {
		sv.Timeline[i].Expanded = true
	}
	m.rebuildViewport()
	view := m.View()
	assertNoHostile(t, view)
	if !strings.Contains(view, "beforeafter") {
		t.Errorf("view lost surrounding text: %q", view)
	}
	for _, e := range sv.Timeline {
		for _, s := range []string{e.Text, e.ToolResult} {
			if strings.ContainsAny(s, "\x1b\x07") {
				t.Errorf("timeline entry %s keeps control chars: %q", e.Kind, s)
			}
		}
	}
	if got := sv.Timeline[0].Text; !strings.Contains(got, "after\tTAB\nnext line") {
		t.Errorf("thinking text lost tab or newline: %q", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestView_SanitizesSplitSequenceAcrossDeltas(t *testing.T) {
	for _, kind := range []agent.EventKind{agent.EventTokenDelta, agent.EventThinkingDelta} {
		m := newSanitizeModel()
		chunks := []string{"start \x1b]5", "2;c;aGVsbG8=\x1b", "\\ end\r", "\nnext \x1b[", "2Jdone"}
		for _, c := range chunks {
			ev := agent.Event{Kind: kind}
			if kind == agent.EventTokenDelta {
				ev.TextDelta = c
			} else {
				ev.ThinkingDelta = c
			}
			m.handleAgentEvent(AgentEventMsg{SessionID: "s1", Ev: ev})
		}
		sv := m.sessions["s1"]
		if len(sv.Timeline) != 1 {
			t.Fatalf("timeline len = %d, want 1", len(sv.Timeline))
		}
		if got, want := sv.Timeline[0].Text, "start  end\nnext done"; got != want {
			t.Errorf("kind %v: text = %q, want %q", kind, got, want)
		}
		assertNoHostile(t, m.View())
	}
}

func TestView_SanitizesDiffPreview(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	content := "line1 \x1b]52;c;aGk=\x07\nold \x1b[2J\nline3 \x1b[35mred\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(map[string]string{
		"path": path, "old_string": "old \x1b[2J", "new_string": "new \x1b]0;t\x07",
	})

	m := newSanitizeModel()
	m.handleAgentEvent(AgentEventMsg{SessionID: "s1", Ev: agent.Event{
		Kind: agent.EventToolCallDone, ToolCallID: "t1", ToolName: "edit", InputJSON: string(input),
	}})
	sv := m.sessions["s1"]
	if strings.ContainsAny(sv.Timeline[0].DiffPreview, "\x1b\x07") {
		t.Errorf("diff preview keeps control chars: %q", sv.Timeline[0].DiffPreview)
	}
	if !strings.Contains(sv.Timeline[0].DiffPreview, "- old") {
		t.Errorf("diff preview lost content: %q", sv.Timeline[0].DiffPreview)
	}
	assertNoHostile(t, m.View())
}

func TestView_SanitizesApprovalPrompt(t *testing.T) {
	input := json.RawMessage(`{"command":"echo \u001b]52;c;aGk=\u0007 \u001b[2J hi"}`)
	for _, sudo := range []bool{false, true} {
		m := newSanitizeModel()
		m.Update(ApprovalReqMsg{
			Req: guard.ApprovalRequest{
				SessionID:        "s1",
				Tool:             "shell",
				Input:            input,
				SuggestedPattern: "echo \x1b[2J",
				PasswordOnly:     sudo,
			},
			Reply: make(chan guard.ApprovalDecision, 1),
		})
		view := m.View()
		assertNoHostile(t, view)
		if !strings.Contains(view, "hi") {
			t.Errorf("sudo=%v: approval lost command text: %q", sudo, view)
		}
	}
}

func TestHistoryToTimeline_Sanitizes(t *testing.T) {
	entries := historyToTimeline(sanitizeHistoryFixture())
	for _, e := range entries {
		for _, s := range []string{e.Text, e.ToolResult} {
			if strings.ContainsAny(s, "\x1b\x07") {
				t.Errorf("entry %s keeps control chars: %q", e.Kind, s)
			}
		}
	}
}

func sanitizeHistoryFixture() []common.Message {
	bad := "x\x1b]52;c;aGk=\x07\x1b[2Jy"
	return []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: bad}}},
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockThinking, Text: bad},
			{Kind: common.BlockText, Text: bad},
			{Kind: common.BlockToolUse, ToolName: "shell", ToolUseID: "t1", InputJSON: `{"command":"` + bad + `"}`},
		}},
		{Role: common.RoleTool, Content: []common.Block{
			{Kind: common.BlockToolResult, ToolUseID: "t1", ResultText: bad},
		}},
	}
}
