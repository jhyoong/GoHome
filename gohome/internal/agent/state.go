package agent

import (
	"sync"

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
	busy       bool
	pending    func() (*session.Session, *session.Writer, error)
	pendingTag string
}

// NewSessionState creates a SessionState with an initial session and writer.
func NewSessionState(sess *session.Session, writer *session.Writer) *SessionState {
	return &SessionState{sess: sess, writer: writer}
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
// function is stored as pending and queued returns true. Otherwise the
// function is executed immediately and the session/writer are replaced.
func (s *SessionState) Swap(tag string, fn func() (*session.Session, *session.Writer, error)) (queued bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		s.pending = fn
		s.pendingTag = tag
		return true, nil
	}
	newSess, newWriter, err := fn()
	if err != nil {
		return false, err
	}
	s.sess = newSess
	s.writer = newWriter
	return false, nil
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
	newSess, newWriter, err := fn()
	if err != nil {
		return tag, err
	}
	s.sess = newSess
	s.writer = newWriter
	return tag, nil
}

// ClearPending discards any pending swap without executing it.
func (s *SessionState) ClearPending() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = nil
	s.pendingTag = ""
}

// Model returns the model name from the current session.
func (s *SessionState) Model() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sess.Model
}

// SetModel updates the model name on the current session.
func (s *SessionState) SetModel(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sess.Model = name
}
