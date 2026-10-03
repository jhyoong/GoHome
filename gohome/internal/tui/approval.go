package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
)

// ApprovalReqMsg is sent by Frontend.RequestApproval into the Bubble Tea loop.
// It is exported so that tests can send it directly via tm.Send.
// The reply channel must be buffered (cap >= 1) so resolving never blocks Update.
type ApprovalReqMsg struct {
	Req   guard.ApprovalRequest
	Reply chan guard.ApprovalDecision
}

// approvalReqMsg is an internal alias so the switch in Update compiles cleanly.
type approvalReqMsg = ApprovalReqMsg

// approvalPrompt holds all UI state for one pending approval request.
type approvalPrompt struct {
	req     guard.ApprovalRequest
	reply   chan guard.ApprovalDecision
	pattern string // current (possibly edited) pattern

	// selected is the currently highlighted menu item (0=Allow once, 1=Allow always,
	// 2=Deny, 3=Deny+steer). Zero-init gives us "Allow once" as the default.
	selected int

	// edit sub-mode: user pressed 'e' to edit the pattern
	editing      bool
	patternInput textinput.Model

	// expandedSummary: user pressed 'v' to see full summary
	expandedSummary bool

	// steer sub-mode: user pressed '4' to deny + steer
	steering   bool
	steerInput textinput.Model

	// sudo password sub-mode: command needs sudo password.
	// sudoStage is set after the user picks Allow; while set, every key goes
	// to passwordInput. sudoOutcome remembers which Allow option was picked.
	needsSudo     bool
	sudoStage     bool
	sudoOutcome   guard.ApprovalOutcome
	sudoErr       string
	passwordInput textinput.Model
}

// newApprovalPrompt builds an approvalPrompt from a request.
func newApprovalPrompt(req guard.ApprovalRequest, reply chan guard.ApprovalDecision) *approvalPrompt {
	pi := textinput.New()
	pi.Placeholder = "pattern"
	pi.SetValue(req.SuggestedPattern)

	si := textinput.New()
	si.Placeholder = "steer message"

	pwi := textinput.New()
	pwi.Prompt = ""
	pwi.EchoMode = textinput.EchoPassword

	return &approvalPrompt{
		req:           req,
		reply:         reply,
		pattern:       req.SuggestedPattern,
		patternInput:  pi,
		steerInput:    si,
		needsSudo:     req.NeedsSudoPassword,
		passwordInput: pwi,
	}
}

// approvalSummaryLine builds a single contextual line describing the tool call
// (e.g. "shell: git status", "read: path/to/file").
func approvalSummaryLine(ap *approvalPrompt, focusedSessionID string) string {
	arg := extractToolArg(ap.req.Tool, string(ap.req.Input))
	prefix := sessionPrefix(ap, focusedSessionID)
	if arg != "" {
		return fmt.Sprintf("%s%s: %s", prefix, ap.req.Tool, arg)
	}
	return prefix + ap.req.Tool
}

// sessionPrefix returns "[sid] " when the request comes from a session other
// than the focused one, and "" otherwise.
func sessionPrefix(ap *approvalPrompt, focusedSessionID string) string {
	if ap.req.SessionID != focusedSessionID {
		return fmt.Sprintf("[%s] ", ap.req.SessionID)
	}
	return ""
}

// capLines keeps at most maxLines lines. When lines are dropped, the last kept
// line is trimmed so that it plus " ..." fits within width columns.
func capLines(lines []string, maxLines, width int) ([]string, bool) {
	if len(lines) <= maxLines {
		return lines, false
	}
	out := append([]string(nil), lines[:maxLines]...)
	out[maxLines-1] = TruncateText(out[maxLines-1], width-4) + " ..."
	return out, true
}

var approvalBoxStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	Padding(0, 1).
	BorderForeground(lipgloss.Color("3"))

// renderApprovalOverlay renders the approval prompt box for the given prompt.
func renderApprovalOverlay(ap *approvalPrompt, width int, focusedSessionID string) string {
	if ap.sudoStage {
		return renderSudoDialog(ap, width, focusedSessionID)
	}

	var sb strings.Builder

	summary := approvalSummaryLine(ap, focusedSessionID)
	boxW := width - 4
	if boxW < 20 {
		boxW = 20
	}
	// Style.Width includes the horizontal padding (1 each side).
	contentW := boxW - 2

	wrapped := WrapText(summary, contentW)
	if ap.expandedSummary {
		sb.WriteString(strings.Join(wrapped, "\n"))
	} else {
		const maxSummaryLines = 3
		capped, truncated := capLines(wrapped, maxSummaryLines, contentW)
		sb.WriteString(strings.Join(capped, "\n"))
		if truncated {
			sb.WriteString("\n(v to expand)")
		}
	}
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
	return approvalBoxStyle.Width(boxW).Render(inner)
}

var (
	sudoBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			Padding(0, 1).
			BorderForeground(lipgloss.Color("9"))
	sudoHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
)

// renderSudoDialog renders the password stage of a sudo approval.
func renderSudoDialog(ap *approvalPrompt, width int, focusedSessionID string) string {
	boxW := width - 4
	if boxW < 20 {
		boxW = 20
	}

	// Style.Width includes the horizontal padding (1 each side).
	contentW := boxW - 2

	header := sessionPrefix(ap, focusedSessionID) + "SUDO PASSWORD REQUIRED"

	const maxCmdLines = 3
	cmdLines := WrapText(extractToolArg(ap.req.Tool, string(ap.req.Input)), contentW)
	cmdLines, _ = capLines(cmdLines, maxCmdLines, contentW)

	const pwLabel = "Password: "
	// Keep the field on one line so long passwords scroll instead of wrapping.
	ap.passwordInput.Width = max(contentW-len(pwLabel)-1, 1)

	var sb strings.Builder
	sb.WriteString(sudoHeaderStyle.Render(header))
	sb.WriteString("\n")
	sb.WriteString(strings.Join(cmdLines, "\n"))
	sb.WriteString("\n\n")
	sb.WriteString(pwLabel)
	sb.WriteString(ap.passwordInput.View())
	sb.WriteString("\n")
	if ap.sudoErr != "" {
		sb.WriteString(sudoHeaderStyle.Render(ap.sudoErr))
	}
	sb.WriteString("\n")
	sb.WriteString("Enter: run | Esc: back | Ctrl+C: deny")

	return sudoBoxStyle.Width(boxW).Render(sb.String())
}
