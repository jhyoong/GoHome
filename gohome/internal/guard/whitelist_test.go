package guard

import (
	"encoding/json"
	"testing"
)

func TestWhitelistFileRoundtrip(t *testing.T) {
	original := WhitelistFile{
		Tools: []string{"read", "write", "edit"},
		Bash:  []string{"^git status", "^ls"},
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

	if len(got.Bash) != len(original.Bash) {
		t.Fatalf("bash length: want %d got %d", len(original.Bash), len(got.Bash))
	}
	for i, pat := range original.Bash {
		if got.Bash[i] != pat {
			t.Errorf("bash[%d]: want %q got %q", i, pat, got.Bash[i])
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
	if len(wf.Bash) != 0 {
		t.Errorf("expected empty bash, got %v", wf.Bash)
	}
}
