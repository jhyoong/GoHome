package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
)

// ApprovalReqMsg is sent into the Bubble Tea loop when a tool call needs
// approval. It carries a resolve callback that delivers the user's decision.
// It is exported so that tests can send it directly via tm.Send.
type ApprovalReqMsg struct {
	Req     guard.ApprovalRequest
	Resolve func(guard.ApprovalDecision)
}

// approvalPrompt holds all UI state for one pending approval request.
type approvalPrompt struct {
	req     guard.ApprovalRequest
	resolve func(guard.ApprovalDecision)
	pattern string // current (possibly edited) pattern

	// selected is the currently highlighted menu item (0=Allow once, 1=Allow always,
	// 2=Deny, 3=Deny+steer). Zero-init gives us "Allow once" as the default.
	selected int

	// edit sub-mode: user pressed 'e' to edit the pattern
	editing      bool
	patternInput textinput.Model

	// steer sub-mode: user pressed '4' to deny + steer
	steering   bool
	steerInput textinput.Model
}

// newApprovalPrompt builds an approvalPrompt from a request and resolve callback.
func newApprovalPrompt(req guard.ApprovalRequest, resolve func(guard.ApprovalDecision)) *approvalPrompt {
	pi := textinput.New()
	pi.Placeholder = "pattern"
	pi.SetValue(req.SuggestedPattern)

	si := textinput.New()
	si.Placeholder = "steer message"

	return &approvalPrompt{
		req:          req,
		resolve:      resolve,
		pattern:      req.SuggestedPattern,
		patternInput: pi,
		steerInput:   si,
	}
}

// approvalSummaryLine builds a single contextual line describing the tool call
// (e.g. "bash: git status", "read: path/to/file").
func approvalSummaryLine(ap *approvalPrompt) string {
	arg := extractToolArg(ap.req.Tool, string(ap.req.Input))
	if arg != "" {
		return fmt.Sprintf("%s: %s", ap.req.Tool, arg)
	}
	return ap.req.Tool
}

var approvalBoxStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	Padding(0, 1).
	BorderForeground(lipgloss.Color("3"))

// approvalSummaryLine builds a single contextual line describing the tool call
// (e.g. "bash: git status", "read: path/to/file").
func approvalSummaryLine(ap *approvalPrompt) string {
	arg := extractToolArg(ap.req.Tool, string(ap.req.Input))
	if arg != "" {
		return fmt.Sprintf("%s: %s", ap.req.Tool, arg)
	}
	return ap.req.Tool
}

// renderApprovalOverlay renders the approval prompt box for the given prompt.
func renderApprovalOverlay(ap *approvalPrompt, width int) string {
	var sb strings.Builder

	sb.WriteString(approvalSummaryLine(ap))
	sb.WriteString("\n")

	if ap.steering {
		sb.WriteString("\nSteer message (Enter to send, Esc to cancel):\n")
		sb.WriteString(ap.steerInput.View())
	} else if ap.editing {
		sb.WriteString("\n  [1] Allow once\n")
		fmt.Fprintf(&sb, "  [2] Allow always  %s\n", ap.patternInput.View())
		sb.WriteString("  [3] Deny\n")
		sb.WriteString("  [4] Deny + steer\n")
		sb.WriteString("(Enter to confirm, Esc to cancel)")
	} else {
		marker := func(i int) string {
			if ap.selected == i {
				return "> "
			}
			return "  "
		}
		fmt.Fprintf(&sb, "\n%s[1] Allow once\n", marker(0))
		if ap.pattern != "" {
			fmt.Fprintf(&sb, "%s[2] Allow always  %s  (e edit)\n", marker(1), ap.pattern)
		} else {
			fmt.Fprintf(&sb, "%s[2] Allow always\n", marker(1))
		}
		fmt.Fprintf(&sb, "%s[3] Deny\n", marker(2))
		fmt.Fprintf(&sb, "%s[4] Deny + steer\n", marker(3))
		sb.WriteString("Esc: deny | arrows to navigate")
	}

	inner := sb.String()

	// Constrain to available width (minus border/padding overhead of ~4 chars).
	boxW := width - 4
	if boxW < 20 {
		boxW = 20
	}
	return approvalBoxStyle.Width(boxW).Render(inner)
}
