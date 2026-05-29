package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Tests for Task 6.7: read-before-edit enforcement via SessionState.

func makeSessionCtx() (context.Context, *fakeSession) {
	sess := newFakeSession("/tmp")
	ctx := WithSession(context.Background(), sess)
	return ctx, sess
}

func execReadWithCtx(t *testing.T, ctx context.Context, input any) Result {
	t.Helper()
	raw, _ := json.Marshal(input)
	rt := &ReadTool{}
	res, err := rt.Execute(ctx, raw, NullSink{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return res
}

func execEditWithCtx(t *testing.T, ctx context.Context, input any) Result {
	t.Helper()
	raw, _ := json.Marshal(input)
	et := &EditTool{}
	res, err := et.Execute(ctx, raw, NullSink{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return res
}

func TestRead_MarksPathInSession(t *testing.T) {
	path := mustTempFile(t, 3)
	ctx, sess := makeSessionCtx()

	res := execReadWithCtx(t, ctx, map[string]any{"path": path})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if !sess.HasRead(path) {
		t.Error("expected session to have path marked as read after ReadTool")
	}
}

func TestEdit_UnreadFileReturnsMustReadFirst(t *testing.T) {
	path := mustWriteTemp(t, "hello world\n")
	ctx, _ := makeSessionCtx()
	// Do NOT read the file first.

	res := execEditWithCtx(t, ctx, map[string]any{
		"path":       path,
		"old_string": "hello",
		"new_string": "goodbye",
	})
	if !res.IsError {
		t.Fatal("expected IsError because file was not read first")
	}
	if !strings.Contains(res.Content, "must be read first") {
		t.Errorf("expected 'must be read first' in error, got %q", res.Content)
	}
}

func TestEdit_AfterReadSucceeds(t *testing.T) {
	path := mustWriteTemp(t, "hello world\n")
	ctx, _ := makeSessionCtx()

	// Read first.
	res := execReadWithCtx(t, ctx, map[string]any{"path": path})
	if res.IsError {
		t.Fatalf("unexpected read error: %s", res.Content)
	}

	// Now edit should succeed.
	res = execEditWithCtx(t, ctx, map[string]any{
		"path":       path,
		"old_string": "hello",
		"new_string": "goodbye",
	})
	if res.IsError {
		t.Fatalf("unexpected edit error: %s", res.Content)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "goodbye world\n" {
		t.Errorf("got %q, want %q", string(got), "goodbye world\n")
	}
}

func TestEdit_NoSessionSkipsCheck(t *testing.T) {
	// Without a session in context, edit should work without requiring a prior read.
	path := mustWriteTemp(t, "hello world\n")

	raw, _ := json.Marshal(map[string]any{
		"path":       path,
		"old_string": "hello",
		"new_string": "goodbye",
	})
	et := &EditTool{}
	res, err := et.Execute(context.Background(), raw, NullSink{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success when no session in context, got: %s", res.Content)
	}
}
