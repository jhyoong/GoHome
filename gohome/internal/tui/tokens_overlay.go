package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// TokensOverlay displays token usage statistics for a session.
// It implements the Interactive interface so it can be used as an activeModal.
type TokensOverlay struct {
	sv            *SessionView
	modelName     string
	contextWindow int
	onClose       func()
}

// NewTokensOverlay creates a TokensOverlay for the given session view.
// onClose is called when the user presses Esc to dismiss the overlay.
func NewTokensOverlay(sv *SessionView, modelName string, contextWindow int, onClose func()) *TokensOverlay {
	return &TokensOverlay{
		sv:            sv,
		modelName:     modelName,
		contextWindow: contextWindow,
		onClose:       onClose,
	}
}

// Render implements Component. It returns a single-element slice containing the
// formatted token usage text -- the same content as Model.renderTokensOverlay().
func (o *TokensOverlay) Render(width int) []string {
	u := o.sv.Usage
	used := u.InputTokens + u.OutputTokens
	total := o.contextWindow
	pct := 0
	if total > 0 {
		pct = int(float64(used) / float64(total) * 100)
		if pct > 100 {
			pct = 100
		}
	}
	modelName := o.modelName
	if modelName == "" {
		modelName = "?"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Token usage -- %s -- %s\n", o.sv.ID, modelName)
	fmt.Fprintf(&sb, "  Input tokens    %d\n", u.InputTokens)
	fmt.Fprintf(&sb, "  Output tokens   %d\n", u.OutputTokens)
	fmt.Fprintf(&sb, "  Cache reads     %d\n", u.CacheReadTokens)
	fmt.Fprintf(&sb, "  Cache writes    %d\n", u.CacheWriteTokens)
	sb.WriteString("  --------------------\n")
	fmt.Fprintf(&sb, "  Total           %d / %d (%d%%)\n", used, total, pct)
	sb.WriteString("  Esc to close")
	return []string{sb.String()}
}

// HandleInput implements Interactive. Esc dismisses the overlay; all other keys
// are ignored.
func (o *TokensOverlay) HandleInput(msg tea.KeyMsg) tea.Cmd {
	if msg.Type == tea.KeyEsc {
		o.onClose()
	}
	return nil
}
