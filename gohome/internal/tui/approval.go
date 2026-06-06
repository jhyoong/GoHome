package tui

import (
	"encoding/json"
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

	// steer sub-mode: user pressed '4' to deny + steer
	steering   bool
	steerInput textinput.Model
}

// newApprovalPrompt builds an approvalPrompt from a request.
func newApprovalPrompt(req guard.ApprovalRequest, reply chan guard.ApprovalDecision) *approvalPrompt {
	pi := textinput.New()
	pi.Placeholder = "pattern"
	pi.SetValue(req.SuggestedPattern)

	si := textinput.New()
	si.Placeholder = "steer message"

	return &approvalPrompt{
		req:          req,
		reply:        reply,
		pattern:      req.SuggestedPattern,
		patternInput: pi,
		steerInput:   si,
	}
}

// bashCommand extracts the "command" field from a bash tool input JSON.
// Returns "" when the tool is not bash or when the field is absent.
func bashCommand(ap *approvalPrompt) string {
	if ap.req.Tool != "bash" {
		return ""
	}
	var v map[string]json.RawMessage
	if err := json.Unmarshal(ap.req.Input, &v); err != nil {
		return ""
	}
	raw, ok := v["command"]
	if !ok {
		return ""
	}
	var cmd string
	if err := json.Unmarshal(raw, &cmd); err != nil {
		return ""
	}
	return cmd
}

var approvalBoxStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	Padding(0, 1).
	BorderForeground(lipgloss.Color("3"))

// renderApprovalOverlay renders the approval prompt box for the given prompt.
func renderApprovalOverlay(ap *approvalPrompt, width int) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "Approve tool call -- %s\n", ap.req.SessionID)
	fmt.Fprintf(&sb, "Tool: %s\n", ap.req.Tool)

	if cmd := bashCommand(ap); cmd != "" {
		fmt.Fprintf(&sb, "Command: %s\n", cmd)
	}

	if ap.steering {
		sb.WriteString("\nSteer message (Enter to send, Esc to cancel):\n")
		sb.WriteString(ap.steerInput.View())
	} else if ap.editing {
		sb.WriteString("\n[1] Allow once\n")
		fmt.Fprintf(&sb, "[2] Allow always   pattern: %s\n", ap.patternInput.View())
		sb.WriteString("[3] Deny\n")
		sb.WriteString("[4] Deny + steer\n")
		sb.WriteString("(Enter to confirm pattern, Esc to cancel edit)")
	} else {
		sb.WriteString("\n[1] Allow once\n")
		fmt.Fprintf(&sb, "[2] Allow always   pattern: %s  (e to edit)\n", ap.pattern)
		sb.WriteString("[3] Deny\n")
		sb.WriteString("[4] Deny + steer\n")
		sb.WriteString("Esc: deny")
	}

	inner := sb.String()

	// Constrain to available width (minus border/padding overhead of ~4 chars).
	boxW := width - 4
	if boxW < 20 {
		boxW = 20
	}
	return approvalBoxStyle.Width(boxW).Render(inner)
}
