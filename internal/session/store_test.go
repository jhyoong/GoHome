package session_test

import (
	"context"
	"testing"

	"github.com/JiaHui/gohome/internal/session"
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

	if err := store.UpdateSessionTitle(ctx, s.ID, "My Custom Title"); err != nil {
		t.Fatalf("UpdateSessionTitle: %v", err)
	}

	sessions, _ := store.ListSessions(ctx)
	if len(sessions) != 1 || sessions[0].Title != "My Custom Title" {
		t.Errorf("title not updated: %+v", sessions)
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
