package guard

import "testing"

func TestDefaultDenyPatterns_NotEmpty(t *testing.T) {
	if len(DefaultDenyPatterns) == 0 {
		t.Error("expected DefaultDenyPatterns to have entries")
	}
}

func TestDefaultDenyPatterns_ContainsSubstringAndRegex(t *testing.T) {
	hasSubstring := false
	hasRegex := false
	for _, p := range DefaultDenyPatterns {
		if len(p) > 6 && p[:6] == "regex:" {
			hasRegex = true
		} else {
			hasSubstring = true
		}
	}
	if !hasSubstring {
		t.Error("expected at least one substring pattern in defaults")
	}
	if !hasRegex {
		t.Error("expected at least one regex pattern in defaults")
	}
}
