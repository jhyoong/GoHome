package tui

import tea "github.com/charmbracelet/bubbletea"

// PasteMsg is sent when a bracketed paste is detected.
// Bubbletea v1.3.10+ handles bracketed paste detection internally,
// delivering the paste as a tea.KeyMsg with Paste=true.
type PasteMsg struct {
	Text string
}

// EnableBracketedPaste returns the ANSI sequence to enable bracketed paste mode.
func EnableBracketedPaste() string {
	return "\x1b[?2004h"
}

// DisableBracketedPaste returns the ANSI sequence to disable bracketed paste mode.
func DisableBracketedPaste() string {
	return "\x1b[?2004l"
}

// IsPasteMsg returns true if the given tea.KeyMsg is a bracketed paste.
func IsPasteMsg(msg tea.KeyMsg) bool {
	return msg.Paste
}
