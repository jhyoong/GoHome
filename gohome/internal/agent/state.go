package agent

import (
	"sync"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

// SessionState guards the current session and writer behind a mutex,
// allowing safe swaps (e.g. /resume, /new) even while the agent loop
// is running. When the agent is busy, a swap is queued as "pending"
// and applied later via DrainPending.
type SessionState struct {
	mu         sync.Mutex
	sess       *session.Session
	writer     *session.Writer
	client     common.Client
	busy       bool
	pending    func(oldSess *session.Session, oldWriter *session.Writer) (*session.Session, *session.Writer, error)
	pendingTag string
	model      ModelSettings
}

// NewSessionState creates a SessionState with an initial session, writer, and
// LLM client. The model settings start with the session's model and config
// name and no limits; use SetModelSettings to set them.
func NewSessionState(sess *session.Session, writer *session.Writer, client common.Client) *SessionState {
	s := &SessionState{sess: sess, writer: writer, client: client}
	if sess != nil {
		s.model = ModelSettings{Name: sess.Model, ConfigName: sess.ModelConfig}
	}
	return s
}

// Session returns the current session.
func (s *SessionState) Session() *session.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sess
}

// Writer returns the current writer.
func (s *SessionState) Writer() *session.Writer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writer
}

// MarkBusy marks the agent as busy (inside a turn). While busy, Swap
// calls queue the swap instead of executing it immediately.
func (s *SessionState) MarkBusy() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.busy = true
}

// MarkIdle marks the agent as idle (between turns).
func (s *SessionState) MarkIdle() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.busy = false
}

// Swap replaces the session and writer. If the agent is busy, the swap
// function is stored as pending (replacing any earlier one) and Swap
// returns nil; DrainPending runs it later. Otherwise the function is
// executed immediately and the session/writer are replaced. The new
// session takes the active model settings, so a resumed session uses the
// current model.
func (s *SessionState) Swap(tag string, fn func(oldSess *session.Session, oldWriter *session.Writer) (*session.Session, *session.Writer, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		s.pending = fn
		s.pendingTag = tag
		return nil
	}
	newSess, newWriter, err := fn(s.sess, s.writer)
	if err != nil {
		return err
	}
	s.sess = newSess
	s.writer = newWriter
	s.syncSessionModel()
	return nil
}

// DrainPending executes and clears any pending swap. Returns the tag
// of the pending swap, or "" if there was nothing pending.
func (s *SessionState) DrainPending() (tag string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == nil {
		return "", nil
	}
	fn := s.pending
	tag = s.pendingTag
	s.pending = nil
	s.pendingTag = ""
	newSess, newWriter, err := fn(s.sess, s.writer)
	if err != nil {
		return tag, err
	}
	s.sess = newSess
	s.writer = newWriter
	s.syncSessionModel()
	return tag, nil
}

// ClearPending discards any pending swap without executing it.
func (s *SessionState) ClearPending() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = nil
	s.pendingTag = ""
}

// ModelSettings is the active model and its per-request limits.
type ModelSettings struct {
	Name            string // model name sent to the LLM
	ConfigName      string // key of the model config in settings
	MaxTokens       int    // if > 0, overrides the default 4096 per-turn token limit
	Thinking        common.ThinkingMode
	ThinkingBudget  int // token budget, used only in budget mode
	ReasoningEffort string
}

// ModelSettings returns the active model settings.
func (s *SessionState) ModelSettings() ModelSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.model
}

// SetModelSettings replaces the active model settings and updates the
// current session's model and config name to match.
func (s *SessionState) SetModelSettings(ms ModelSettings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.model = ms
	s.syncSessionModel()
}

// syncSessionModel copies the active model name and config name onto the
// current session. Callers must hold s.mu.
func (s *SessionState) syncSessionModel() {
	if s.sess == nil {
		return
	}
	s.sess.Model = s.model.Name
	s.sess.ModelConfig = s.model.ConfigName
}

// Model returns the active model name.
func (s *SessionState) Model() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.model.Name
}

// ModelConfig returns the active model config name.
func (s *SessionState) ModelConfig() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.model.ConfigName
}

// Client returns the current LLM client.
func (s *SessionState) Client() common.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client
}

// SetClient replaces the current LLM client.
func (s *SessionState) SetClient(c common.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.client = c
}
