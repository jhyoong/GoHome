package session

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

func decodeMap(t *testing.T, b []byte) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, b)
	}
	return m
}

func assertStringField(t *testing.T, m map[string]json.RawMessage, key, want string) {
	t.Helper()
	raw, ok := m[key]
	if !ok {
		t.Errorf("key %q missing", key)
		return
	}
	var got string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Errorf("key %q unmarshal: %v", key, err)
		return
	}
	if got != want {
		t.Errorf("key %q: got %q, want %q", key, got, want)
	}
}

func assertBoolField(t *testing.T, m map[string]json.RawMessage, key string, want bool) {
	t.Helper()
	raw, ok := m[key]
	if !ok {
		t.Errorf("key %q missing", key)
		return
	}
	var got bool
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Errorf("key %q unmarshal: %v", key, err)
		return
	}
	if got != want {
		t.Errorf("key %q: got %v, want %v", key, got, want)
	}
}

func assertTSPresent(t *testing.T, m map[string]json.RawMessage) {
	t.Helper()
	raw, ok := m["ts"]
	if !ok {
		t.Fatal("ts field missing")
	}
	var tsStr string
	if err := json.Unmarshal(raw, &tsStr); err != nil {
		t.Fatalf("ts unmarshal: %v", err)
	}
	if _, err := time.Parse(time.RFC3339, tsStr); err != nil {
		t.Errorf("ts %q is not RFC3339: %v", tsStr, err)
	}
}

func TestEncodeSessionStart(t *testing.T) {
	ev := SessionStart{ID: "s1", ParentID: "p1", CWD: "/home", Model: "gpt-4o", Endpoint: "https://x", Depth: 2}
	b, err := Encode(ev)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	m := decodeMap(t, b)
	assertStringField(t, m, "type", "session_start")
	assertTSPresent(t, m)
	assertStringField(t, m, "id", "s1")
	assertStringField(t, m, "parentId", "p1")
	assertStringField(t, m, "cwd", "/home")
	assertStringField(t, m, "model", "gpt-4o")
}

func TestEncodeUserMessage(t *testing.T) {
	ev := UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "hello"}}}
	b, err := Encode(ev)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	m := decodeMap(t, b)
	assertStringField(t, m, "type", "user_message")
	assertTSPresent(t, m)
	if _, ok := m["content"]; !ok {
		t.Error("content field missing")
	}
}

func TestEncodeAssistantMessage(t *testing.T) {
	u := &common.Usage{InputTokens: 10, OutputTokens: 5}
	ev := AssistantMessage{
		Content:    []common.Block{{Kind: common.BlockText, Text: "hi"}},
		StopReason: "end_turn",
		Usage:      u,
	}
	b, err := Encode(ev)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	m := decodeMap(t, b)
	assertStringField(t, m, "type", "assistant_message")
	assertStringField(t, m, "stopReason", "end_turn")
	assertTSPresent(t, m)
}

func TestEncodeToolResult(t *testing.T) {
	ev := ToolResult{ToolUseID: "tu1", Content: "output", IsError: false}
	b, err := Encode(ev)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	m := decodeMap(t, b)
	assertStringField(t, m, "type", "tool_result")
	assertStringField(t, m, "toolUseId", "tu1")
	assertStringField(t, m, "content", "output")
}

func TestEncodeApproval(t *testing.T) {
	ev := Approval{ToolUseID: "tu2", Outcome: "allow", SavedPattern: "", SteerMessage: ""}
	b, err := Encode(ev)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	m := decodeMap(t, b)
	assertStringField(t, m, "type", "approval")
	assertStringField(t, m, "outcome", "allow")
	// savedPattern is omitempty — may be absent
}

func TestEncodeSubagentSpawn(t *testing.T) {
	ev := SubagentSpawn{ToolUseID: "tu3", ChildID: "child1", Task: "do something"}
	b, err := Encode(ev)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	m := decodeMap(t, b)
	assertStringField(t, m, "type", "subagent_spawn")
	assertStringField(t, m, "childId", "child1")
	assertStringField(t, m, "task", "do something")
}

func TestEncodeSubagentDone(t *testing.T) {
	ev := SubagentDone{ToolUseID: "tu4", ChildID: "child1", IsError: true}
	b, err := Encode(ev)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	m := decodeMap(t, b)
	assertStringField(t, m, "type", "subagent_done")
	assertBoolField(t, m, "isError", true)
}

func TestEncodeSessionEnd(t *testing.T) {
	ev := SessionEnd{Reason: "done"}
	b, err := Encode(ev)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	m := decodeMap(t, b)
	assertStringField(t, m, "type", "session_end")
	assertStringField(t, m, "reason", "done")
}

func TestEncodeUnknownType(t *testing.T) {
	_, err := Encode(struct{ X string }{X: "??"})
	if err == nil {
		t.Error("expected error for unknown type, got nil")
	}
}
