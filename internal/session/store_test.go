package session_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jhyoong/gohome/internal/session"
)

func TestOpenAndMigrate(t *testing.T) {
	store, err := session.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
}

func TestSessionCRUD(t *testing.T) {
	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()

	s, err := store.CreateSession(ctx)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if s.ID == "" {
		t.Error("empty session ID")
	}

	sessions, err := store.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("want 1 session, got %d", len(sessions))
	}

	if err := store.DeleteSession(ctx, s.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	sessions, _ = store.ListSessions(ctx)
	if len(sessions) != 0 {
		t.Errorf("want 0 sessions after delete, got %d", len(sessions))
	}
}

func TestMessageCRUD(t *testing.T) {
	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()

	s, _ := store.CreateSession(ctx)
	msg, err := store.AddMessage(ctx, session.Message{
		SessionID: s.ID,
		Role:      "user",
		Content:   "hello",
	})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	if msg.ID == "" {
		t.Error("empty message ID")
	}

	msgs, err := store.GetMessages(ctx, s.ID)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Errorf("unexpected messages: %+v", msgs)
	}
}

func TestUpdateSessionTitle(t *testing.T) {
	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()

	s, _ := store.CreateSession(ctx)
	if s.Title != "New Session" {
		t.Fatalf("unexpected default title: %q", s.Title)
	}
	createdAt := s.UpdatedAt

	time.Sleep(time.Second)

	if err := store.UpdateSessionTitle(ctx, s.ID, "My Custom Title"); err != nil {
		t.Fatalf("UpdateSessionTitle: %v", err)
	}

	sessions, _ := store.ListSessions(ctx)
	if len(sessions) != 1 || sessions[0].Title != "My Custom Title" {
		t.Errorf("title not updated: %+v", sessions)
	}
	if !sessions[0].UpdatedAt.After(createdAt) {
		t.Errorf("updated_at not bumped: was %v, now %v", createdAt, sessions[0].UpdatedAt)
	}
}

func TestToolResultCRUD(t *testing.T) {
	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()

	s, _ := store.CreateSession(ctx)
	msg, _ := store.AddMessage(ctx, session.Message{SessionID: s.ID, Role: "assistant"})
	tr, err := store.AddToolResult(ctx, session.ToolResult{
		MessageID: msg.ID,
		ToolName:  "shell",
		Params:    `{"command":"ls"}`,
		Result:    "file.txt",
		Approved:  true,
	})
	if err != nil {
		t.Fatalf("AddToolResult: %v", err)
	}
	results, err := store.GetToolResults(ctx, msg.ID)
	if err != nil {
		t.Fatalf("GetToolResults: %v", err)
	}
	if len(results) != 1 || results[0].ID != tr.ID {
		t.Errorf("unexpected tool results: %+v", results)
	}
}

func TestMessageThinkingField(t *testing.T) {
	// Test: Message struct with Thinking field serializes to JSON with "thinking" key
	msg := session.Message{
		SessionID: "test-session",
		Role:      "assistant",
		Content:   "Hello",
		Thinking:  "some thinking content",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	// Parse the JSON back to check for "thinking" field
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	thinkingVal, ok := parsed["thinking"]
	if !ok {
		t.Error("Message JSON does not contain 'thinking' field")
	}
	if thinkingVal != "some thinking content" {
		t.Errorf("thinking field value: got %v, want %v", thinkingVal, "some thinking content")
	}
}

func TestAddMessageWithThinking(t *testing.T) {
	// Test: AddMessage stores thinking content in the database
	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()

	s, _ := store.CreateSession(ctx)
	thinkingContent := "I need to think about this carefully"

	msg, err := store.AddMessage(ctx, session.Message{
		SessionID: s.ID,
		Role:      "assistant",
		Content:   "Let me think...",
		Thinking:  thinkingContent,
	})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	if msg.Thinking != thinkingContent {
		t.Errorf("thinking: got %q, want %q", msg.Thinking, thinkingContent)
	}
}

func TestGetMessagesWithThinking(t *testing.T) {
	// Test: GetMessages returns messages with thinking content intact
	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()

	s, _ := store.CreateSession(ctx)
	thinkingContent := "my internal reasoning"

	_, err := store.AddMessage(ctx, session.Message{
		SessionID: s.ID,
		Role:      "assistant",
		Content:   "Answer",
		Thinking:   thinkingContent,
	})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	msgs, err := store.GetMessages(ctx, s.ID)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Thinking != thinkingContent {
		t.Errorf("Thinking: got %q, want %q", msgs[0].Thinking, thinkingContent)
	}
}

func TestMessageThinkingEmpty(t *testing.T) {
	// Test: Message without Thinking field has empty string (not omitted from JSON)
	msg := session.Message{
		SessionID: "test-session",
		Role:      "user",
		Content:   "Hello",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// thinking field should be present (empty string), not omitted
	_, ok := parsed["thinking"]
	if !ok {
		t.Error("Message JSON should contain 'thinking' field even when empty")
	}
}
