package config

import (
	"encoding/json"
	"testing"
)

// Task 2.1: Endpoint + Settings structs
func TestSettings_ParseEndpoint(t *testing.T) {
	raw := `{"endpoints":{"e1":{"wire":"anthropic","baseURL":"http://x","apiKeyEnv":"K","defaultModel":"m","contextWindow":200000}},"defaultEndpoint":"e1"}`

	var s Settings
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	e, ok := s.Endpoints["e1"]
	if !ok {
		t.Fatal("expected endpoint 'e1' to exist")
	}
	if e.Wire != WireAnthropic {
		t.Errorf("Wire: got %q, want %q", e.Wire, WireAnthropic)
	}
	if e.ContextWindow != 200000 {
		t.Errorf("ContextWindow: got %d, want 200000", e.ContextWindow)
	}
	if s.DefaultEndpoint != "e1" {
		t.Errorf("DefaultEndpoint: got %q, want %q", s.DefaultEndpoint, "e1")
	}
}
