package rpc

import (
	"encoding/json"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

// ---------- Method constants ----------

const (
	MethodAgentEvent      = "agent.event"
	MethodSessionState    = "session.state"
	MethodSessionInput    = "session.input"
	MethodSessionNew      = "session.new"
	MethodSessionResume   = "session.resume"
	MethodSessionList     = "session.list"
	MethodSessionCancel   = "session.cancel"
	MethodModelSet        = "model.set"
	MethodDaemonHealth    = "daemon.health"
	MethodDaemonStop      = "daemon.stop"
	MethodApprovalRequest = "approval.request"
)

// ---------- Daemon -> TUI Notifications ----------

// AgentEventParams carries a single agent event from daemon to TUI.
type AgentEventParams struct {
	SessionID string      `json:"sessionID"`
	Event     agent.Event `json:"event"`
}

// SessionStateParams carries a full session state snapshot from daemon to TUI.
// Timeline is encoded as json.RawMessage to avoid importing the tui package.
type SessionStateParams struct {
	SessionID       string                 `json:"sessionID"`
	Model           string                 `json:"model"`
	Yolo            bool                   `json:"yolo"`
	Timeline        json.RawMessage        `json:"timeline"`
	PendingApproval *ApprovalRequestParams `json:"pendingApproval,omitempty"`
}

// ---------- TUI -> Daemon Requests ----------

// SessionInputParams carries user text input for a session.
type SessionInputParams struct {
	SessionID string `json:"sessionID"`
	Text      string `json:"text"`
}

// SessionResumeParams carries the session ID to resume.
type SessionResumeParams struct {
	ID string `json:"id"`
}

// ModelSetParams carries the model name to switch to.
type ModelSetParams struct {
	Name string `json:"name"`
}

// ModelSetResult carries the result of a model switch.
type ModelSetResult struct {
	ModelName     string `json:"modelName"`
	ContextWindow int    `json:"contextWindow"`
}

// SessionNewResult carries the ID of a newly created session.
type SessionNewResult struct {
	SessionID string `json:"sessionID"`
}

// SessionResumeResult carries the resumed session state.
type SessionResumeResult struct {
	SessionID string          `json:"sessionID"`
	History   json.RawMessage `json:"history"`
}

// SessionListResult carries the list of available sessions.
type SessionListResult struct {
	Sessions []session.Listing `json:"sessions"`
}

// SessionCancelParams carries the session ID to cancel.
type SessionCancelParams struct {
	SessionID string `json:"sessionID"`
}

// HealthResult carries daemon health information.
type HealthResult struct {
	Version       string `json:"version"`
	UptimeSeconds int64  `json:"uptimeSeconds"`
}

// ---------- Daemon -> TUI Requests ----------

// ApprovalRequestParams is sent from daemon to TUI when a tool call needs approval.
type ApprovalRequestParams struct {
	SessionID        string          `json:"sessionID"`
	Tool             string          `json:"tool"`
	Input            json.RawMessage `json:"input"`
	Summary          string          `json:"summary"`
	SuggestedPattern string          `json:"suggestedPattern"`
}

// ApprovalResponseResult carries the user's decision back to the daemon.
type ApprovalResponseResult struct {
	Decision guard.ApprovalDecision `json:"decision"`
}
