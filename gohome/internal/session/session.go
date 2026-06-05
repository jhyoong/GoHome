package session

import (
	"sync"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// Session holds all state for one agent session.
type Session struct {
	ID        string
	Depth     int
	ParentID  string
	Model     string
	Endpoint  string
	History   []common.Message
	StartedAt time.Time

	cwd       string
	mu        sync.RWMutex
	readFiles map[string]struct{}
}

// NewSession creates a new Session with the given parameters.
// StartedAt is set to time.Now().UTC().
func NewSession(id, cwd, model, endpoint string) *Session {
	return &Session{
		ID:        id,
		cwd:       cwd,
		Model:     model,
		Endpoint:  endpoint,
		StartedAt: time.Now().UTC(),
		readFiles: make(map[string]struct{}),
	}
}

// MarkRead records that path has been read in this session.
// Safe for concurrent use.
func (s *Session) MarkRead(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readFiles[path] = struct{}{}
}

// HasRead reports whether path was previously marked as read.
// Safe for concurrent use.
func (s *Session) HasRead(path string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.readFiles[path]
	return ok
}

// CWD returns the working directory for this session.
func (s *Session) CWD() string {
	return s.cwd
}
