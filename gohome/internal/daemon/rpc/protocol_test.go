package rpc

import (
	"encoding/json"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
)

func TestAgentEventParams_RoundTrip(t *testing.T) {
	orig := AgentEventParams{
		SessionID: "sess-123",
		Event: agent.Event{
			Kind:      agent.EventTokenDelta,
			SessionID: "sess-123",
			TextDelta: "hello world",
		},
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got AgentEventParams
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.SessionID != orig.SessionID {
		t.Errorf("SessionID = %q, want %q", got.SessionID, orig.SessionID)
	}
	if got.Event.Kind != orig.Event.Kind {
		t.Errorf("Event.Kind = %q, want %q", got.Event.Kind, orig.Event.Kind)
	}
	if got.Event.TextDelta != orig.Event.TextDelta {
		t.Errorf("Event.TextDelta = %q, want %q", got.Event.TextDelta, orig.Event.TextDelta)
	}
	if got.Event.SessionID != orig.Event.SessionID {
		t.Errorf("Event.SessionID = %q, want %q", got.Event.SessionID, orig.Event.SessionID)
	}
}

func TestSessionInputParams_RoundTrip(t *testing.T) {
	orig := SessionInputParams{
		SessionID: "sess-456",
		Text:      "please fix the bug",
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got SessionInputParams
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.SessionID != orig.SessionID {
		t.Errorf("SessionID = %q, want %q", got.SessionID, orig.SessionID)
	}
	if got.Text != orig.Text {
		t.Errorf("Text = %q, want %q", got.Text, orig.Text)
	}
}

func TestHealthResult_RoundTrip(t *testing.T) {
	orig := HealthResult{
		Version:       "0.3.0-dev",
		UptimeSeconds: 3600,
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got HealthResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Version != orig.Version {
		t.Errorf("Version = %q, want %q", got.Version, orig.Version)
	}
	if got.UptimeSeconds != orig.UptimeSeconds {
		t.Errorf("UptimeSeconds = %d, want %d", got.UptimeSeconds, orig.UptimeSeconds)
	}
}
