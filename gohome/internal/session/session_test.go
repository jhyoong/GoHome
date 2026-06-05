package session

import (
	"sync"
	"testing"
	"time"
)

func TestNewSession(t *testing.T) {
	before := time.Now().UTC()
	s := NewSession("id1", "/tmp/work", "gpt-4o", "https://api.example.com")
	after := time.Now().UTC()

	if s.ID != "id1" {
		t.Errorf("ID: got %q, want %q", s.ID, "id1")
	}
	if s.CWD() != "/tmp/work" {
		t.Errorf("CWD: got %q, want %q", s.CWD(), "/tmp/work")
	}
	if s.Model != "gpt-4o" {
		t.Errorf("Model: got %q, want %q", s.Model, "gpt-4o")
	}
	if s.Endpoint != "https://api.example.com" {
		t.Errorf("Endpoint: got %q, want %q", s.Endpoint, "https://api.example.com")
	}
	if s.StartedAt.Before(before) || s.StartedAt.After(after) {
		t.Errorf("StartedAt %v not in [%v, %v]", s.StartedAt, before, after)
	}
	if s.Depth != 0 {
		t.Errorf("Depth: got %d, want 0", s.Depth)
	}
	if s.ParentID != "" {
		t.Errorf("ParentID: got %q, want empty", s.ParentID)
	}
}

func TestMarkReadHasRead(t *testing.T) {
	s := NewSession("id2", "/tmp/x", "m", "e")

	if s.HasRead("/tmp/x/foo.go") {
		t.Error("HasRead returned true before MarkRead")
	}
	s.MarkRead("/tmp/x/foo.go")
	if !s.HasRead("/tmp/x/foo.go") {
		t.Error("HasRead returned false after MarkRead")
	}
	if s.HasRead("/tmp/x/bar.go") {
		t.Error("HasRead returned true for unread path")
	}
}

func TestMarkReadConcurrent(t *testing.T) {
	s := NewSession("id3", "/tmp/y", "m", "e")
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := "/tmp/y/file.go"
			s.MarkRead(path)
			_ = s.HasRead(path)
		}(i)
	}
	wg.Wait()
}
