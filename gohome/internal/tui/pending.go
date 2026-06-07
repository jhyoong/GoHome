package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var pendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)

type PendingMessagesComponent struct {
	messages *[]string
}

func NewPendingMessages(messages *[]string) *PendingMessagesComponent {
	return &PendingMessagesComponent{messages: messages}
}

func (p *PendingMessagesComponent) Render(width int) []string {
	if p.messages == nil || len(*p.messages) == 0 {
		return nil
	}
	var lines []string
	lines = append(lines, pendingStyle.Render("Queued:"))
	for i, msg := range *p.messages {
		prefix := fmt.Sprintf("  [%d] ", i+1)
		maxW := width - VisualWidth(prefix)
		if maxW < 10 {
			maxW = 10
		}
		text := msg
		if VisualWidth(text) > maxW {
			text = TruncateText(text, maxW-3) + "..."
		}
		lines = append(lines, pendingStyle.Render(prefix+text))
	}
	return lines
}
