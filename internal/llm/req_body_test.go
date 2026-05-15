package llm

import (
	"encoding/json"
	"testing"
)

// TestReqBodyThinkingTokensField tests that reqBody struct serializes ThinkingTokens field to JSON
// with the key "thinking_tokens". This tests acceptance criterion T001-1.
func TestReqBodyThinkingTokensField(t *testing.T) {
	body := reqBody{ThinkingTokens: 42}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var parsed struct {
		ThinkingTokens int `json:"thinking_tokens"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if parsed.ThinkingTokens != 42 {
		t.Errorf("ThinkingTokens: got %d, want 42", parsed.ThinkingTokens)
	}
}