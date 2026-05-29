package guard

import (
	"context"
	"encoding/json"
	"sync/atomic"
)

// Frontend is the interface the guard engine calls when a tool use requires
// human approval. It is defined locally so guard does not import agent or TUI.
type Frontend interface {
	RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalDecision, error)
}

// Guard is the runtime guardrail engine that checks every tool call against
// the whitelist and, when required, prompts the frontend for approval.
type Guard struct {
	whitelist *Whitelist
	frontend  Frontend
	yolo      atomic.Bool
}

// NewGuard creates a Guard backed by the given Whitelist and Frontend.
func NewGuard(wl *Whitelist, fe Frontend) *Guard {
	return &Guard{
		whitelist: wl,
		frontend:  fe,
	}
}

// Check decides whether the given tool call is allowed.
// It short-circuits for yolo mode and whitelisted calls without contacting
// the frontend. Otherwise it builds an ApprovalRequest and maps the decision.
func (g *Guard) Check(ctx context.Context, sessionID, tool string, input json.RawMessage) (Decision, error) {
	// 1. Yolo: allow everything, no frontend call.
	if g.yolo.Load() {
		return Decision{Allow: true, Reason: "yolo"}, nil
	}

	// 2. Whitelisted: allow, no frontend call.
	if g.whitelist.Allows(tool, input) {
		return Decision{Allow: true, Reason: "whitelisted"}, nil
	}

	// 3. Build approval request and ask the frontend.
	summary := summaryFor(tool, input)
	req := ApprovalRequest{
		SessionID:        sessionID,
		Tool:             tool,
		Input:            input,
		Summary:          summary,
		SuggestedPattern: Suggest(tool, input),
	}

	dec, err := g.frontend.RequestApproval(ctx, req)
	if err != nil {
		return Decision{}, err
	}

	switch dec.Outcome {
	case AllowOnce:
		return Decision{Allow: true, Reason: "user_once"}, nil

	case AllowAlways:
		// Persist to the project whitelist file and update in-memory state.
		if err := g.whitelist.AddProject(tool, dec.SavedPattern); err != nil {
			// Log but don't fail the allow — the user said yes.
			// Future calls will re-prompt, which is acceptable.
			_ = err
		}
		return Decision{Allow: true, Reason: "user_always"}, nil

	case Deny:
		return Decision{Allow: false, Reason: "user_denied"}, nil

	case DenySteer:
		return Decision{Allow: false, Reason: "user_denied_steer", SteerMessage: dec.SteerMessage}, nil

	default:
		return Decision{Allow: false, Reason: "unknown_outcome"}, nil
	}
}

// summaryFor builds a short human-readable summary of a tool call.
// For bash it returns the command string; for other tools it returns the tool name.
func summaryFor(tool string, input json.RawMessage) string {
	if tool == "bash" {
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(input, &args); err == nil && args.Command != "" {
			return args.Command
		}
	}
	return tool
}
