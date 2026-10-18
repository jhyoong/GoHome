package session

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_ModelChangeUpdatesModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	w, err := OpenWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	w.Emit(SessionStart{ID: "s1", CWD: "/tmp", Model: "old", ModelConfig: "old-cfg", StartedAt: time.Now()})
	w.Emit(ModelChange{Model: "new", ModelConfig: "new-cfg"})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	sess, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if sess.Model != "new" || sess.ModelConfig != "new-cfg" {
		t.Errorf("model = %q/%q, want new/new-cfg", sess.Model, sess.ModelConfig)
	}
}
