package session

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// NewID returns a random 8-character lowercase base32 session identifier.
func NewID() string {
	buf := make([]byte, 5)
	if _, err := rand.Read(buf); err != nil {
		panic("session.NewID: crypto/rand failed: " + err.Error())
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	return strings.ToLower(enc.EncodeToString(buf))
}

// Session holds all state for one agent session.
type Session struct {
	ID          string
	Depth       int
	ParentID    string
	Model       string
	ModelConfig string
	History     []common.Message
	StartedAt   time.Time

	// ParentToolUseID is the ID of the parent's subagent tool_use block that
	// spawned this session. Empty for top-level sessions.
	ParentToolUseID string

	cwd       string
	mu        sync.RWMutex
	readFiles map[string]struct{}

	// subagents counts subagents spawned by this session, including those
	// recorded in its file before a resume, so child IDs never repeat.
	subagents int
}

// NewSession creates a new Session with the given parameters.
// StartedAt is set to time.Now().UTC().
func NewSession(id, cwd, model, modelConfig string) *Session {
	return &Session{
		ID:          id,
		cwd:         cwd,
		Model:       model,
		ModelConfig: modelConfig,
		StartedAt:   time.Now().UTC(),
		readFiles:   make(map[string]struct{}),
	}
}

// normalizeReadPath returns a clean absolute form of path so that
// "foo.go", "./foo.go" and an absolute path to the same file match.
// Relative paths resolve against the process working directory.
// Symlinks are resolved so a link and its target match. If the file does
// not exist, the parent folder is resolved and the base name joined back.
// If that also fails, the clean absolute path is used.
func normalizeReadPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
		return filepath.Join(dir, filepath.Base(abs))
	}
	return abs
}

// MarkRead records that path has been read in this session.
// Safe for concurrent use.
func (s *Session) MarkRead(path string) {
	key := normalizeReadPath(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readFiles[key] = struct{}{}
}

// HasRead reports whether path was previously marked as read.
// Safe for concurrent use.
func (s *Session) HasRead(path string) bool {
	key := normalizeReadPath(path)
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.readFiles[key]
	return ok
}

// NextSubagentID returns a new child session ID of the form
// "<ID>-sub-N". Safe for concurrent use.
func (s *Session) NextSubagentID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subagents++
	return fmt.Sprintf("%s-sub-%d", s.ID, s.subagents)
}

// NoteSubagentID raises the subagent count to N when id is "<ID>-sub-N", so
// NextSubagentID never returns an ID already used on disk. Other IDs are
// ignored. Safe for concurrent use.
func (s *Session) NoteSubagentID(id string) {
	rest, ok := strings.CutPrefix(id, s.ID+"-sub-")
	if !ok {
		return
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if n > s.subagents {
		s.subagents = n
	}
}

// CWD returns the working directory for this session.
func (s *Session) CWD() string {
	return s.cwd
}
