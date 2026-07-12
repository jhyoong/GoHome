package guard

import (
	"encoding/json"
	"testing"
)

func TestWhitelistFileRoundtrip(t *testing.T) {
	original := WhitelistFile{
		Tools: []string{"read", "write", "edit"},
		Shell: []string{"^git status", "^ls"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got WhitelistFile
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(got.Tools) != len(original.Tools) {
		t.Fatalf("tools length: want %d got %d", len(original.Tools), len(got.Tools))
	}
	for i, tool := range original.Tools {
		if got.Tools[i] != tool {
			t.Errorf("tools[%d]: want %q got %q", i, tool, got.Tools[i])
		}
	}

	if len(got.Shell) != len(original.Shell) {
		t.Fatalf("shell length: want %d got %d", len(original.Shell), len(got.Shell))
	}
	for i, pat := range original.Shell {
		if got.Shell[i] != pat {
			t.Errorf("shell[%d]: want %q got %q", i, pat, got.Shell[i])
		}
	}
}

func TestWhitelistFileEmptyFields(t *testing.T) {
	data := []byte(`{}`)
	var wf WhitelistFile
	if err := json.Unmarshal(data, &wf); err != nil {
		t.Fatalf("unmarshal empty: %v", err)
	}
	if len(wf.Tools) != 0 {
		t.Errorf("expected empty tools, got %v", wf.Tools)
	}
	if len(wf.Shell) != 0 {
		t.Errorf("expected empty shell, got %v", wf.Shell)
	}
}
