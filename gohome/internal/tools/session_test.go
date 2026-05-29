package tools

import (
	"context"
	"testing"
)

// fakeSession is a test implementation of SessionState.
type fakeSession struct {
	read map[string]bool
	cwd  string
}

func newFakeSession(cwd string) *fakeSession {
	return &fakeSession{read: make(map[string]bool), cwd: cwd}
}

func (s *fakeSession) MarkRead(path string) { s.read[path] = true }
func (s *fakeSession) HasRead(path string) bool { return s.read[path] }
func (s *fakeSession) CWD() string              { return s.cwd }

func TestSessionRoundTrip(t *testing.T) {
	sess := newFakeSession("/tmp/work")
	ctx := WithSession(context.Background(), sess)

	got := SessionFrom(ctx)
	if got == nil {
		t.Fatal("SessionFrom returned nil on a context with session")
	}
	if got.CWD() != "/tmp/work" {
		t.Errorf("CWD: got %q, want %q", got.CWD(), "/tmp/work")
	}
}

func TestSessionFrom_NilOnBareContext(t *testing.T) {
	got := SessionFrom(context.Background())
	if got != nil {
		t.Fatalf("expected nil SessionState on bare context, got %v", got)
	}
}

func TestSessionMarkAndHasRead(t *testing.T) {
	sess := newFakeSession("/")
	ctx := WithSession(context.Background(), sess)

	s := SessionFrom(ctx)
	if s.HasRead("/tmp/foo.go") {
		t.Error("HasRead should be false before MarkRead")
	}
	s.MarkRead("/tmp/foo.go")
	if !s.HasRead("/tmp/foo.go") {
		t.Error("HasRead should be true after MarkRead")
	}
}
