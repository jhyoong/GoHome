package tui

import "strings"

var helpLines = []string{
	"Keyboard shortcuts",
	"  Ctrl+C        Cancel turn / double-tap to quit",
	"  Ctrl+E        Open external editor",
	"  Ctrl+H        Toggle this help",
	"  Ctrl+]        Focus next session",
	"  Ctrl+[        Focus prev session",
	"  PgUp/PgDown   Scroll chat (or this help)",
	"  Up/Down       Scroll this help",
	"  Enter         Submit input / toggle tool detail",
	"  Alt+Enter     Insert newline",
	"  Tab           Autocomplete / confirm file search",
	"  Esc           Close overlay / cancel",
	"  @             File search",
	"",
	"Slash commands",
	"  /help         Show this help",
	"  /new          Start a new session",
	"  /resume       Resume a past session",
	"  /yolo         Toggle YOLO mode (skip approvals)",
	"  /endpoint     Switch endpoint",
	"  /model        Switch model",
	"  /cancel       Cancel current turn",
	"  /tokens       Show token usage",
	"  /quit         Quit gohome",
	"",
	"CLI flags",
	"  --endpoint    Endpoint name override",
	"  --model       Model override",
	"  --yolo        Disable all approval prompts",
	"  --resume      Resume most recent session",
	"  --version     Print version and exit",
}

func (m *Model) renderHelpOverlay(maxH int) string {
	total := len(helpLines)

	// Reserve one line for the footer.
	viewH := maxH - 1
	if viewH < 1 {
		viewH = 1
	}

	// Clamp scroll offset.
	maxScroll := total - viewH
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.helpScroll > maxScroll {
		m.helpScroll = maxScroll
	}

	end := m.helpScroll + viewH
	if end > total {
		end = total
	}
	visible := helpLines[m.helpScroll:end]

	var sb strings.Builder
	sb.WriteString(strings.Join(visible, "\n"))
	sb.WriteString("\n")

	if maxScroll == 0 || m.helpScroll >= maxScroll {
		sb.WriteString("Esc to close · --END--")
	} else {
		sb.WriteString("Esc to close · Press down for more")
	}
	return sb.String()
}
