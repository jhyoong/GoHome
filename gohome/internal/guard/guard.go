package guard

import "encoding/json"

// ApprovalOutcome is the decision returned by the Frontend for a pending tool call.
type ApprovalOutcome string

const (
	AllowOnce   ApprovalOutcome = "allow_once"
	AllowAlways ApprovalOutcome = "allow_always"
	Deny        ApprovalOutcome = "deny"
	DenySteer   ApprovalOutcome = "deny_steer"
)

// ApprovalRequest is sent to the Frontend when a tool call is not whitelisted.
type ApprovalRequest struct {
	SessionID        string
	Tool             string
	Input            json.RawMessage
	Summary          string
	SuggestedPattern string
}

// ApprovalDecision is the response from the Frontend for a pending tool call.
type ApprovalDecision struct {
	Outcome      ApprovalOutcome
	SavedPattern string
	SteerMessage string
}

// Decision is the final result of Guard.Check(), consumed by the agent.
type Decision struct {
	Allow        bool
	Reason       string
	SteerMessage string
}
