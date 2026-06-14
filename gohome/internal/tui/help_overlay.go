package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// HelpOverlay is a standalone scrollable help panel implementing Interactive.
type HelpOverlay struct {
	scroll  int
	maxH    int
	onClose func()
}

// NewHelpOverlay creates a HelpOverlay with the given viewport height and close
// callback. The callback is invoked when the user presses Esc.
func NewHelpOverlay(maxH int, onClose func()) *HelpOverlay {
	return &HelpOverlay{maxH: maxH, onClose: onClose}
}

// SetMaxH updates the viewport height (called when the window resizes).
func (o *HelpOverlay) SetMaxH(h int) { o.maxH = h }

// Render returns the visible slice of helpLines plus a footer hint.
func (o *HelpOverlay) Render(width int) []string {
	total := len(helpLines)

	// Reserve one line for the footer.
	viewH := o.maxH - 1
	if viewH < 1 {
		viewH = 1
	}

	// Clamp scroll offset.
	maxScroll := total - viewH
	if maxScroll < 0 {
		maxScroll = 0
	}
	if o.scroll > maxScroll {
		o.scroll = maxScroll
	}

	end := o.scroll + viewH
	if end > total {
		end = total
	}
	visible := helpLines[o.scroll:end]

	var sb strings.Builder
	sb.WriteString(strings.Join(visible, "\n"))
	sb.WriteString("\n")

	if maxScroll == 0 || o.scroll >= maxScroll {
		sb.WriteString("Esc to close · --END--")
	} else {
		sb.WriteString("Esc to close · Press down for more")
	}

	return []string{sb.String()}
}

// HandleInput processes key events for scrolling and closing.
func (o *HelpOverlay) HandleInput(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		o.onClose()
	case tea.KeyUp, tea.KeyLeft:
		if o.scroll > 0 {
			o.scroll--
		}
	case tea.KeyDown, tea.KeyRight:
		o.scroll++
	case tea.KeyPgUp:
		o.scroll -= 5
		if o.scroll < 0 {
			o.scroll = 0
		}
	case tea.KeyPgDown:
		o.scroll += 5
	}
	return nil
}
