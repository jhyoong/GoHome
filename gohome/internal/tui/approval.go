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

	// toolLabel is req.Tool with control characters shown as text, for
	// display. req.Tool keeps the raw value for comparisons.
	toolLabel string

	// arg is the tool argument (e.g. the shell command) with control
	// characters shown as text. hasControls is true when the tool name or
	// argument held any; the prompt then shows controlCharWarning.
	arg         string
	hasControls bool

	// selected is the currently highlighted menu item (0=Allow once, 1=Allow always,
	// 2=Deny, 3=Deny+steer). Zero-init gives us "Allow once" as the default.
	selected int

	// edit sub-mode: user pressed 'e' to edit the pattern
	editing      bool
	patternInput textinput.Model
	patternErr   string // validation error shown while editing

	// expandedSummary: user pressed 'v' (menu) or Tab (password dialog) to
	// see the full summary
	expandedSummary bool

	// scroll is the first visible line of the expanded summary. scrollMax is
	// the largest valid scroll, set at render time; 0 means everything fits.
	scroll    int
	scrollMax int
	scrollWin int // visible summary lines when scrollable

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
	// The suggested pattern comes from the command text. A pattern with
	// control characters is not offered: removing them could also remove
	// escaping backslashes and widen the rule.
	pattern := req.SuggestedPattern
	if sanitizeText(pattern) != pattern {
		pattern = ""
	}
	toolLabel, toolCtl := showControls(req.Tool)
	rawArg, fallback := rawToolArg(req.Tool, string(req.Input))
	arg, argCtl := showControls(rawArg)
	if fallback {
		arg = shortSummary(arg)
	}

	pi := textinput.New()
	pi.Placeholder = "pattern"
	pi.SetValue(pattern)

	si := textinput.New()
	si.Placeholder = "steer message"

	pwi := textinput.New()
	pwi.Prompt = ""
	pwi.EchoMode = textinput.EchoPassword

	ap := &approvalPrompt{
		req:           req,
		reply:         reply,
		pattern:       pattern,
		toolLabel:     toolLabel,
		arg:           arg,
		hasControls:   toolCtl || argCtl,
		patternInput:  pi,
		steerInput:    si,
		needsSudo:     req.NeedsSudoPassword,
		passwordInput: pwi,
	}
	// Whitelisted sudo commands skip the menu and open the password stage.
	if req.PasswordOnly {
		ap.sudoStage = true
		ap.sudoOutcome = guard.AllowOnce
		ap.passwordInput.Focus()
	}
	return ap
}

// approvalSummaryLine builds a single contextual line describing the tool call
// (e.g. "shell: git status", "read: path/to/file").
func approvalSummaryLine(ap *approvalPrompt, focusedSessionID string) string {
	prefix := sessionPrefix(ap, focusedSessionID)
	if ap.arg != "" {
		return fmt.Sprintf("%s%s: %s", prefix, ap.toolLabel, ap.arg)
	}
	return prefix + ap.toolLabel
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
// maxHeight is the most lines the box may use; 0 means no limit. An expanded
// summary that does not fit is shown as a window that PgUp/PgDn move.
func renderApprovalOverlay(ap *approvalPrompt, width, maxHeight int, focusedSessionID string) string {
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
	var tail strings.Builder
	if ap.steering {
		tail.WriteString("\nSteer message (Enter to send, Esc to cancel):\n")
		tail.WriteString(ap.steerInput.View())
	} else if ap.editing {
		tail.WriteString("\n  [1] Allow once\n")
		fmt.Fprintf(&tail, "  [2] Allow always  %s\n", ap.patternInput.View())
		tail.WriteString("  [3] Deny\n")
		tail.WriteString("  [4] Deny + steer\n")
		if ap.patternErr != "" {
			fmt.Fprintf(&tail, "Error: %s\n", ap.patternErr)
		}
		tail.WriteString("(Enter to confirm, Esc to cancel)")
	} else {
		marker := func(i int) string {
			if ap.selected == i {
				return "> "
			}
			return "  "
		}
		fmt.Fprintf(&tail, "\n%s[1] Allow once\n", marker(0))
		if ap.pattern != "" {
			fmt.Fprintf(&tail, "%s[2] Allow always  %s  (e edit)\n", marker(1), ap.pattern)
		} else {
			fmt.Fprintf(&tail, "%s[2] Allow always\n", marker(1))
		}
		fmt.Fprintf(&tail, "%s[3] Deny\n", marker(2))
		fmt.Fprintf(&tail, "%s[4] Deny + steer\n", marker(3))
		if ap.patternErr != "" {
			fmt.Fprintf(&tail, "Error: %s\n", ap.patternErr)
		}
		tail.WriteString("Esc: deny | arrows to navigate")
	}

	// Lines left for the summary: the box minus its border, the blank line
	// before the options, the options and the control warning.
	avail := 0
	if maxHeight > 0 {
		used := 2 + 1 + strings.Count(tail.String(), "\n") + 1
		if ap.hasControls {
			used++
		}
		avail = max(maxHeight-used, 1)
	}

	ap.scrollMax = 0
	if ap.expandedSummary {
		if avail > 0 && len(wrapped) > avail {
			// Keep one line for the position hint.
			win := max(avail-1, 1)
			ap.scrollMax = len(wrapped) - win
			ap.scrollWin = win
			ap.scroll = min(max(ap.scroll, 0), ap.scrollMax)
			sb.WriteString(strings.Join(wrapped[ap.scroll:ap.scroll+win], "\n"))
			fmt.Fprintf(&sb, "\n(lines %d-%d of %d, PgUp/PgDn to scroll)", ap.scroll+1, ap.scroll+win, len(wrapped))
		} else {
			sb.WriteString(strings.Join(wrapped, "\n"))
		}
	} else {
		const maxSummaryLines = 3
		capped, truncated := capLines(wrapped, maxSummaryLines, contentW)
		sb.WriteString(strings.Join(capped, "\n"))
		if truncated {
			sb.WriteString("\n(v to expand)")
		}
	}
	sb.WriteString("\n")
	if ap.hasControls {
		sb.WriteString(sudoHeaderStyle.Render(controlCharWarning) + "\n")
	}
	sb.WriteString(tail.String())

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
	cmdLines := WrapText(ap.arg, contentW)
	truncated := false
	if !ap.expandedSummary {
		cmdLines, truncated = capLines(cmdLines, maxCmdLines, contentW)
	}

	const pwLabel = "Password: "
	// Keep the field on one line so long passwords scroll instead of wrapping.
	ap.passwordInput.Width = max(contentW-len(pwLabel)-1, 1)

	var sb strings.Builder
	sb.WriteString(sudoHeaderStyle.Render(header))
	sb.WriteString("\n")
	sb.WriteString(strings.Join(cmdLines, "\n"))
	sb.WriteString("\n")
	if ap.hasControls {
		sb.WriteString(sudoHeaderStyle.Render(controlCharWarning) + "\n")
	}
	sb.WriteString("\n")
	sb.WriteString(pwLabel)
	sb.WriteString(ap.passwordInput.View())
	sb.WriteString("\n")
	if ap.sudoErr != "" {
		sb.WriteString(sudoHeaderStyle.Render(ap.sudoErr))
	}
	sb.WriteString("\n")
	if ap.req.PasswordOnly {
		sb.WriteString("Enter: run | Esc: deny")
	} else {
		sb.WriteString("Enter: run | Esc: back | Ctrl+C: deny")
	}
	if truncated {
		sb.WriteString(" | Tab: full command")
	}

	return sudoBoxStyle.Width(boxW).Render(sb.String())
}

// controlCharWarning is shown in the approval prompt and sudo dialog when the
// tool call holds control characters.
const controlCharWarning = "Warning: command contains control characters"
