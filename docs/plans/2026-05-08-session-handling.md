# Session Handling Improvement Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Prevent blank sessions from being created on page load, and auto-generate LLM-based titles for new sessions from the first user message.

**Architecture:** The frontend defers session creation until the user sends their first message. The backend detects an empty `session_id` in the `message` WS handler, creates the session, sends back `session_created` + `sessions`, runs the agent, then fires a goroutine to call the LLM for a short title and broadcast the updated sessions list.

**Tech Stack:** Go (net/http, gorilla/websocket, database/sql + SQLite), Vanilla JS (no framework)

---

### Task 1: Add `UpdateSessionTitle` to the session store

**Files:**
- Modify: `internal/session/store.go`
- Modify: `internal/session/store_test.go`

**Step 1: Write the failing test**

Add to `internal/session/store_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

```
go test ./internal/session/... -run TestUpdateSessionTitle -v
```

Expected: FAIL with "undefined: store.UpdateSessionTitle"

**Step 3: Implement `UpdateSessionTitle`**

Add to the end of `internal/session/store.go`:

```go
func (s *Store) UpdateSessionTitle(ctx context.Context, id, title string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET title = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, title, id)
	return err
}
```

**Step 4: Run test to verify it passes**

```
go test ./internal/session/... -run TestUpdateSessionTitle -v
```

Expected: PASS

**Step 5: Run full test suite**

```
go test ./...
```

Expected: all PASS

**Step 6: Commit**

```bash
git add internal/session/store.go internal/session/store_test.go
git commit -m "feat: add UpdateSessionTitle to session store"
```

---

### Task 2: Add `GenerateTitle` to `agent.Loop`

**Files:**
- Modify: `internal/agent/loop.go`
- Modify: `internal/agent/loop_test.go`

**Step 1: Write the failing test**

Add to `internal/agent/loop_test.go`:

```go
func TestGenerateTitle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "  Find Files in Directory  "}},
			},
		})
	}))
	defer srv.Close()

	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()

	loop := agent.NewLoop(
		llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"}),
		tools.NewRegistry(), store, "",
	)

	title, err := loop.GenerateTitle(context.Background(), "find all files in the current directory")
	if err != nil {
		t.Fatalf("GenerateTitle: %v", err)
	}
	if title != "Find Files in Directory" {
		t.Errorf("got %q, want %q", title, "Find Files in Directory")
	}
}

func TestGenerateTitleEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "   "}},
			},
		})
	}))
	defer srv.Close()

	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()

	loop := agent.NewLoop(
		llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"}),
		tools.NewRegistry(), store, "",
	)

	_, err := loop.GenerateTitle(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for empty LLM response")
	}
}
```

**Step 2: Run tests to verify they fail**

```
go test ./internal/agent/... -run TestGenerateTitle -v
```

Expected: FAIL with "undefined: loop.GenerateTitle"

**Step 3: Implement `GenerateTitle`**

Add to `internal/agent/loop.go` (after the existing imports, add `"strings"` if not present):

```go
func (l *Loop) GenerateTitle(ctx context.Context, message string) (string, error) {
	resp, err := l.llm.Complete(ctx, []llm.Message{
		{
			Role:    "system",
			Content: "Generate a short title of at most 10 words for a conversation starting with the following user message. Reply with only the title, no quotes or trailing punctuation.",
		},
		{Role: "user", Content: message},
	}, nil)
	if err != nil {
		return "", err
	}
	title := strings.TrimSpace(resp.Content)
	if title == "" {
		return "", fmt.Errorf("empty title from LLM")
	}
	return title, nil
}
```

Make sure `"strings"` is in the import block of `loop.go`.

**Step 4: Run tests to verify they pass**

```
go test ./internal/agent/... -run TestGenerateTitle -v
```

Expected: both PASS

**Step 5: Run full test suite**

```
go test ./...
```

Expected: all PASS

**Step 6: Commit**

```bash
git add internal/agent/loop.go internal/agent/loop_test.go
git commit -m "feat: add GenerateTitle to agent.Loop"
```

---

### Task 3: Backend WS — `list_sessions` handler and lazy session creation

**Files:**
- Modify: `internal/server/server.go`

**Step 1: Add `list_sessions` to the dispatcher**

In `server.go`, inside the `dispatcher` method's `switch msg.Type` block, add a new case before `case "new_session":`:

```go
case "list_sessions":
    sessions, _ := wc.store.ListSessions(ctx)
    if sessions == nil {
        sessions = []session.Session{}
    }
    wc.send(outMsg{Type: "sessions", Data: sessions})
```

**Step 2: Modify the `message` handler to auto-create session when `session_id` is empty**

Replace the `case "message":` block (lines ~253–273 in `server.go`) with:

```go
case "message":
    wc.mu.Lock()
    if wc.running {
        ch := wc.steerCh
        wc.mu.Unlock()
        if ch != nil {
            select {
            case ch <- msg.Content:
            default:
            }
        }
        continue
    }
    steerCh := make(chan string, 8)
    runCtx, cancel := context.WithCancel(ctx)
    wc.running = true
    wc.steerCh = steerCh
    wc.runCancel = cancel
    wc.mu.Unlock()

    sessionID := msg.SessionID
    isNew := false
    if sessionID == "" {
        sess, err := wc.store.CreateSession(ctx)
        if err != nil {
            wc.send(outMsg{Type: "error", Message: err.Error()})
            wc.mu.Lock()
            wc.running = false
            wc.runCancel = nil
            wc.steerCh = nil
            wc.mu.Unlock()
            continue
        }
        sessionID = sess.ID
        isNew = true
        sessions, _ := wc.store.ListSessions(ctx)
        if sessions == nil {
            sessions = []session.Session{}
        }
        wc.send(outMsg{Type: "session_created", SessionID: sessionID})
        wc.send(outMsg{Type: "sessions", Data: sessions})
    }
    go wc.runAgent(runCtx, sessionID, msg.Content, steerCh, isNew)
```

**Step 3: Update `runAgent` signature and add title generation**

Change the `runAgent` signature from:
```go
func (wc *wsConn) runAgent(ctx context.Context, sessionID, content string, steerCh chan string) {
```
to:
```go
func (wc *wsConn) runAgent(ctx context.Context, sessionID, content string, steerCh chan string, isNew bool) {
```

After the `wc.send(outMsg{Type: "done", MessageID: ""})` line and before the final `sessions, _ := wc.store.ListSessions(ctx)` block, add:

```go
if isNew && wc.loop != nil {
    go func() {
        tCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        title, err := wc.loop.GenerateTitle(tCtx, content)
        if err != nil {
            log.Printf("GenerateTitle: %v", err)
            return
        }
        if err := wc.store.UpdateSessionTitle(tCtx, sessionID, title); err != nil {
            log.Printf("UpdateSessionTitle: %v", err)
            return
        }
        sessions, _ := wc.store.ListSessions(tCtx)
        if sessions == nil {
            sessions = []session.Session{}
        }
        wc.send(outMsg{Type: "sessions", Data: sessions})
    }()
}
```

**Step 4: Verify compilation**

```
go build ./...
```

Expected: no errors

**Step 5: Run full test suite**

```
go test ./...
```

Expected: all PASS

**Step 6: Commit**

```bash
git add internal/server/server.go
git commit -m "feat: lazy session creation and LLM title generation on first message"
```

---

### Task 4: Frontend — lazy creation, `session_created`, New Chat refactor

**Files:**
- Modify: `web/static/app.js`

**Step 1: Change `ws.onopen` to use `list_sessions` instead of `new_session`**

Find this block in `ws.onopen`:
```js
ws.onopen = () => {
    retryDelay = 1000;
    if (activeSessionId) {
      send({ type: 'load_session', session_id: activeSessionId });
    } else {
      send({ type: 'new_session' });
    }
};
```

Replace with:
```js
ws.onopen = () => {
    retryDelay = 1000;
    if (activeSessionId) {
      send({ type: 'load_session', session_id: activeSessionId });
    } else {
      send({ type: 'list_sessions' });
    }
};
```

**Step 2: Handle `session_created` in the message switch**

In `ws.onmessage`, inside the `switch (msg.type)` block, add a new case:

```js
case 'session_created':
    activeSessionId = msg.session_id;
    renderSessions(state.sessions);
    break;
```

Place it alongside the other cases (e.g. after `case 'history'`).

**Step 3: Refactor the New Chat button to clear state locally**

Find:
```js
dom.newChatBtn.addEventListener('click', () => send({ type: 'new_session' }));
```

Replace with:
```js
dom.newChatBtn.addEventListener('click', () => {
    activeSessionId = null;
    state.messages = [];
    dom.messages.innerHTML = '';
    setBusy(false);
    renderSessions(state.sessions);
});
```

This clears the chat view and removes the active highlight without creating a blank session.

**Step 4: Verify the send form handles null `activeSessionId`**

The form submit already sends:
```js
send({ type: 'message', session_id: activeSessionId, content });
```

When `activeSessionId` is `null`, JSON serializes this as `"session_id": null`. In Go, decoding `null` into a `string` field yields `""` — so the backend correctly detects the empty session_id. No change needed here.

**Step 5: Build and run the app**

```
make run
```

Open `http://localhost:8080` in a browser. Verify:

1. Fresh page load → sidebar shows existing sessions, chat area is blank, no new session created in sidebar.
2. Type a message and send → a new session appears in the sidebar, chat shows the message + agent response, session title updates after the agent finishes.
3. Click "New Chat" → chat area clears, no session in sidebar highlighted, no blank session added.
4. Click an existing session → its messages load correctly.
5. Refresh page → empty state again (no auto-created session).

**Step 6: Commit**

```bash
git add web/static/app.js
git commit -m "feat: lazy session creation and LLM title on frontend"
```
