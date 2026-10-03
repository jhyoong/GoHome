package guard

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
)

// Frontend is the interface the guard engine calls when a tool use requires
// human approval.
type Frontend interface {
	RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalDecision, error)
}

// Guard is the runtime guardrail engine that checks every tool call against
// the denylist, whitelist, and when required, prompts the frontend for approval.
type Guard struct {
	denylist  *Denylist
	whitelist *Whitelist
	frontend  Frontend
	yolo      atomic.Bool
}

// NewGuard creates a Guard backed by the given Whitelist, Frontend, and Denylist.
// A nil denylist disables deny checking.
func NewGuard(wl *Whitelist, fe Frontend, dl *Denylist) *Guard {
	return &Guard{
		denylist:  dl,
		whitelist: wl,
		frontend:  fe,
	}
}

// Check decides whether the given tool call is allowed.
// Order: denylist (hard reject) -> yolo -> whitelist -> prompt user.
// Whitelisted sudo commands still ask the frontend for the password.
func (g *Guard) Check(ctx context.Context, sessionID, tool string, input json.RawMessage) (Decision, error) {
	// 1. Denylist: reject immediately, overrides everything.
	if g.denylist != nil {
		if matched, pattern := g.denylist.Denies(tool, input); matched {
			return Decision{
				Allow:    false,
				Reason:   "denylisted",
				DenyInfo: fmt.Sprintf("command denied by denylist: matched pattern '%s'", pattern),
			}, nil
		}
	}

	// 2. Yolo: allow everything, no frontend call.
	if g.yolo.Load() {
		return Decision{Allow: true, Reason: "yolo"}, nil
	}

	summary := summaryFor(tool, input)
	needsSudo := tool == "shell" && IsSudoCommand(summary)
	req := ApprovalRequest{
		SessionID:         sessionID,
		Tool:              tool,
		Input:             input,
		Summary:           summary,
		NeedsSudoPassword: needsSudo,
	}

	// 3. Whitelisted: allow. Sudo commands still need the password.
	if g.whitelist.Allows(tool, input) {
		if !needsSudo {
			return Decision{Allow: true, Reason: "whitelisted"}, nil
		}
		return g.askSudoPassword(ctx, req)
	}

	// 4. Ask the frontend for approval.
	req.SuggestedPattern = Suggest(tool, input)
	dec, err := g.frontend.RequestApproval(ctx, req)
	if err != nil {
		return Decision{}, err
	}

	switch dec.Outcome {
	case AllowOnce:
		return Decision{Allow: true, Reason: "user_once", SudoPassword: dec.SudoPassword}, nil

	case AllowAlways:
		if err := g.whitelist.AddProject(tool, dec.SavedPattern); err != nil {
			slog.Warn("whitelist persist failed", "err", err)
		}
		return Decision{Allow: true, Reason: "user_always", SavedPattern: dec.SavedPattern, SudoPassword: dec.SudoPassword}, nil

	case Deny:
		return Decision{Allow: false, Reason: "user_denied"}, nil

	case DenySteer:
		return Decision{Allow: false, Reason: "user_denied_steer", SteerMessage: dec.SteerMessage}, nil

	default:
		return Decision{Allow: false, Reason: "unknown_outcome"}, nil
	}
}

// askSudoPassword asks the frontend only for the sudo password of a
// whitelisted command. No whitelist entry is added.
func (g *Guard) askSudoPassword(ctx context.Context, req ApprovalRequest) (Decision, error) {
	req.PasswordOnly = true
	dec, err := g.frontend.RequestApproval(ctx, req)
	if err != nil {
		return Decision{}, err
	}
	if dec.Outcome == AllowOnce || dec.Outcome == AllowAlways {
		return Decision{Allow: true, Reason: "whitelisted", SudoPassword: dec.SudoPassword}, nil
	}
	return Decision{Allow: false, Reason: "user_denied"}, nil
}

// summaryFor builds a short human-readable summary of a tool call.
func summaryFor(tool string, input json.RawMessage) string {
	if tool == "shell" {
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(input, &args); err == nil && args.Command != "" {
			return args.Command
		}
	}
	return tool
}
