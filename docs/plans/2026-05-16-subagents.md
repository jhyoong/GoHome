# Subagents Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Let the main agent spawn subagent sessions that run a full LLM+tool loop, stream their activity into a collapsible block in the main chat, and return their final text as a tool result.

**Architecture:** A `SpawnSubagentTool` lives in `internal/agent/subagent_tool.go`, is constructed per-run in `server.go` with the connection's broker and a send-function adapter, and is added to a per-run clone of the global registry. Child sessions are stored in SQLite with `parent_session_id` set; `ListSessions` only returns top-level sessions. The frontend tracks open subagent blocks by session ID and streams events into collapsible panels.

**Tech Stack:** Go 1.25, modernc.org/sqlite, gorilla/websocket, vanilla JS (no build step)

---

### Task 1: DB — add parent_session_id column

**Files:**
- Modify: `internal/session/schema.sql`
- Modify: `internal/session/store.go`
- Modify: `internal/session/store_test.go`

**Step 1: Write failing tests**

Add to the bottom of `internal/session/store_test.go`:

```go
func TestCreateChildSession(t *testing.T) {
	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()

	parent, err := store.CreateSession(ctx)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	child, err := store.CreateChildSession(ctx, parent.ID)
	if err != nil {
		t.Fatalf("CreateChildSession: %v", err)
	}
	if child.ID == "" {
		t.Error("empty child session ID")
	}

	// ListSessions must only return top-level sessions
	sessions, err := store.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("want 1 top-level session, got %d", len(sessions))
	}
	if sessions[0].ID != parent.ID {
		t.Errorf("expected parent session in list, got %s", sessions[0].ID)
	}
}

func TestChildSessionCascadeDelete(t *testing.T) {
	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()

	parent, _ := store.CreateSession(ctx)
	_, _ = store.CreateChildSession(ctx, parent.ID)

	if err := store.DeleteSession(ctx, parent.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	sessions, _ := store.ListSessions(ctx)
	if len(sessions) != 0 {
		t.Errorf("want 0 sessions after cascade delete, got %d", len(sessions))
	}
}
```

**Step 2: Run tests to confirm they fail**

```bash
go test ./internal/session/... -run "TestCreateChildSession|TestChildSessionCascadeDelete" -v
```

Expected: FAIL — `store.CreateChildSession` undefined.

**Step 3: Update schema.sql**

Add `parent_session_id` to the sessions table:

```sql
CREATE TABLE IF NOT EXISTS sessions (
    id                TEXT PRIMARY KEY,
    title             TEXT NOT NULL DEFAULT 'New Session',
    parent_session_id TEXT REFERENCES sessions(id) ON DELETE CASCADE,
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

(Leave the rest of schema.sql unchanged.)

**Step 4: Update store.go**

Add the migration for existing databases. Find the `migrations` slice in `Open()` and add the new entry:

```go
migrations := []string{
    `ALTER TABLE messages ADD COLUMN thinking TEXT`,
    `ALTER TABLE sessions ADD COLUMN parent_session_id TEXT REFERENCES sessions(id) ON DELETE CASCADE`,
}
```

Update `ListSessions` to filter top-level sessions only:

```go
func (s *Store) ListSessions(ctx context.Context) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, created_at, updated_at FROM sessions WHERE parent_session_id IS NULL ORDER BY updated_at DESC`)
```

Add `CreateChildSession` after `CreateSession`:

```go
func (s *Store) CreateChildSession(ctx context.Context, parentID string) (*Session, error) {
	id := uuid.New().String()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, parent_session_id) VALUES (?, ?)`, id, parentID); err != nil {
		return nil, err
	}
	return s.getSession(ctx, id)
}
```

**Step 5: Run tests to confirm they pass**

```bash
go test ./internal/session/... -v
```

Expected: all PASS.

**Step 6: Commit**

```bash
git add internal/session/schema.sql internal/session/store.go internal/session/store_test.go
git commit -m "feat(session): add parent_session_id for subagent sessions"
```

---

### Task 2: Config — add SubagentSystemPrompt field

**Files:**
- Modify: `internal/config/config.go`

**Step 1: Add the field**

In the `Config` struct, add after `SystemPrompt`:

```go
SubagentSystemPrompt string `yaml:"subagent_system_prompt"`
```

**Step 2: Verify tests still pass**

```bash
go test ./internal/config/... -v
```

Expected: all PASS (new field is optional with zero value).

**Step 3: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add subagent_system_prompt field"
```

---

### Task 3: Registry — add CloneWith method

**Files:**
- Modify: `internal/tools/registry.go`
- Modify: `internal/tools/registry_test.go`

**Step 1: Write failing test**

Add to `internal/tools/registry_test.go`:

```go
func TestRegistryCloneWith(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&ShellTool{})

	extra := &FileReadTool{}
	clone := reg.CloneWith(extra)

	// Original unchanged
	if _, ok := reg.Get("file_read"); ok {
		t.Error("original registry should not have file_read")
	}

	// Clone has both
	if _, ok := clone.Get("shell"); !ok {
		t.Error("clone missing shell tool")
	}
	if _, ok := clone.Get("file_read"); !ok {
		t.Error("clone missing file_read tool")
	}
}
```

**Step 2: Run test to confirm it fails**

```bash
go test ./internal/tools/... -run TestRegistryCloneWith -v
```

Expected: FAIL — `CloneWith` undefined.

**Step 3: Add CloneWith to registry.go**

Add after the `All()` method:

```go
func (r *Registry) CloneWith(extra ...Tool) *Registry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	clone := &Registry{tools: make(map[string]Tool)}
	for name, t := range r.tools {
		clone.tools[name] = t
	}
	for _, t := range extra {
		clone.tools[t.Name()] = t
	}
	return clone
}
```

**Step 4: Run tests to confirm they pass**

```bash
go test ./internal/tools/... -v
```

Expected: all PASS.

**Step 5: Commit**

```bash
git add internal/tools/registry.go internal/tools/registry_test.go
git commit -m "feat(tools): add Registry.CloneWith for per-run tool sets"
```

---

### Task 4: SpawnSubagentTool — implement the tool and SubagentEvents interface

**Files:**
- Create: `internal/agent/subagent_tool.go`
- Modify: `internal/agent/loop_test.go` (verify package still compiles)

**Step 1: Create internal/agent/subagent_tool.go**

```go
package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jhyoong/gohome/internal/approval"
	"github.com/jhyoong/gohome/internal/llm"
	"github.com/jhyoong/gohome/internal/session"
	"github.com/jhyoong/gohome/internal/tools"
)

// SubagentEvents receives streaming events from a running subagent.
// Implemented by the server per WebSocket connection.
type SubagentEvents interface {
	OnStart(sessionID, parentID string)
	OnToken(sessionID, token string)
	OnThinkingToken(sessionID, token string)
	OnToolResult(sessionID, tool, params, result string, approved bool)
	OnDone(sessionID, finalText string)
	OnError(sessionID, errMsg string)
}

// SpawnSubagentTool is a Tool that runs a subagent loop for a delegated task.
// It must be constructed per-run (not shared across connections) because it
// captures the parent session ID and the per-connection broker and events.
type SpawnSubagentTool struct {
	llm          *llm.Client
	registry     *tools.Registry
	store        *session.Store
	broker       *approval.Broker
	events       SubagentEvents
	systemPrompt string
	parentID     string
}

func NewSpawnSubagentTool(
	client *llm.Client,
	reg *tools.Registry,
	store *session.Store,
	broker *approval.Broker,
	events SubagentEvents,
	systemPrompt string,
	parentID string,
) *SpawnSubagentTool {
	return &SpawnSubagentTool{
		llm:          client,
		registry:     reg,
		store:        store,
		broker:       broker,
		events:       events,
		systemPrompt: systemPrompt,
		parentID:     parentID,
	}
}

func (t *SpawnSubagentTool) Name() string { return "spawn_subagent" }

func (t *SpawnSubagentTool) Description() string {
	return "Spawn a subagent to handle a delegated task. The subagent runs independently and returns its final response. Use this to delegate work and reduce context pressure."
}

func (t *SpawnSubagentTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"task": {
				"type": "string",
				"description": "A complete, self-contained description of the task for the subagent to complete"
			}
		},
		"required": ["task"]
	}`)
}

func (t *SpawnSubagentTool) Execute(ctx context.Context, params json.RawMessage) (string, error) {
	var p struct {
		Task string `json:"task"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return "", fmt.Errorf("invalid params: %w", err)
	}
	if p.Task == "" {
		return "", fmt.Errorf("task is required")
	}

	child, err := t.store.CreateChildSession(ctx, t.parentID)
	if err != nil {
		return "", fmt.Errorf("creating child session: %w", err)
	}

	t.events.OnStart(child.ID, t.parentID)

	var finalText string
	loop := NewLoop(t.llm, t.registry, t.store, t.systemPrompt)
	err = loop.Run(ctx, child.ID, "", p.Task, t.broker,
		func(token string) {
			finalText += token
			t.events.OnToken(child.ID, token)
		},
		func(errMsg string) {
			t.events.OnError(child.ID, errMsg)
		},
		func(tool, toolParams, result string, approved bool) {
			t.events.OnToolResult(child.ID, tool, toolParams, result, approved)
		},
		nil, // subagents do not support steering
		nil, // usage not forwarded to main session
		func(token string) {
			t.events.OnThinkingToken(child.ID, token)
		},
	)
	if err != nil {
		t.events.OnError(child.ID, err.Error())
		return "", err
	}

	t.events.OnDone(child.ID, finalText)
	return finalText, nil
}
```

**Step 2: Confirm the package compiles**

```bash
go build ./internal/agent/...
```

Expected: builds cleanly.

**Step 3: Run existing agent tests**

```bash
go test ./internal/agent/... -v
```

Expected: all PASS (new file adds no test breakage).

**Step 4: Commit**

```bash
git add internal/agent/subagent_tool.go
git commit -m "feat(agent): add SpawnSubagentTool and SubagentEvents interface"
```

---

### Task 5: Server — refactor Config and wire per-run loop with SpawnSubagentTool

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/server_test.go`
- Modify: `cmd/agent/main.go`

**Step 1: Update server.Config**

Replace the `Loop *agent.Loop` field with the three components the server now manages itself. The new `Config` struct:

```go
type Config struct {
	Store                *session.Store
	LLMClient            *llm.Client
	Registry             *tools.Registry
	SystemPrompt         string
	SubagentSystemPrompt string
	Approval             config.ApprovalConfig
	FullConfig           *config.Config
	ConfigPath           string
	ContextWindow        int
}
```

Add the missing imports at the top of server.go:
```go
"github.com/jhyoong/gohome/internal/llm"
"github.com/jhyoong/gohome/internal/tools"
```

**Step 2: Remove loop field from wsConn**

Remove `loop *agent.Loop` from the `wsConn` struct.

Remove `loop: s.cfg.Loop,` from the `wsConn` literal in `handleWebSocket`.

**Step 3: Add wsSubagentEvents adapter**

Add this struct and its methods in server.go (before `runAgent`):

```go
type wsSubagentEvents struct {
	wc *wsConn
}

func (e *wsSubagentEvents) OnStart(sessionID, parentID string) {
	e.wc.send(outMsg{Type: "subagent_start", SessionID: sessionID, Data: parentID})
}

func (e *wsSubagentEvents) OnToken(sessionID, token string) {
	e.wc.send(outMsg{Type: "subagent_token", SessionID: sessionID, Data: token})
}

func (e *wsSubagentEvents) OnThinkingToken(sessionID, token string) {
	e.wc.send(outMsg{Type: "subagent_thinking_token", SessionID: sessionID, Data: token})
}

func (e *wsSubagentEvents) OnToolResult(sessionID, tool, params, result string, approved bool) {
	e.wc.send(outMsg{
		Type:     "subagent_tool_result",
		SessionID: sessionID,
		Tool:     tool,
		Params:   json.RawMessage(params),
		Result:   result,
		Approved: approved,
	})
}

func (e *wsSubagentEvents) OnDone(sessionID, finalText string) {
	e.wc.send(outMsg{Type: "subagent_done", SessionID: sessionID, Message: finalText})
}

func (e *wsSubagentEvents) OnError(sessionID, errMsg string) {
	e.wc.send(outMsg{Type: "subagent_error", SessionID: sessionID, Message: errMsg})
}
```

**Step 4: Rewrite runAgent to build a per-run loop**

Replace the existing `runAgent` function body. The new version (keep the same signature):

```go
func (wc *wsConn) runAgent(ctx context.Context, sessionID, content string, steerCh chan string, isNew bool) {
	contextWindow := wc.server.cfg.ContextWindow
	defer func() {
		wc.mu.Lock()
		wc.running = false
		wc.runCancel = nil
		wc.steerCh = nil
		wc.mu.Unlock()
	}()

	if wc.server.cfg.LLMClient == nil {
		return
	}

	spawnTool := agent.NewSpawnSubagentTool(
		wc.server.cfg.LLMClient,
		wc.server.cfg.Registry,
		wc.server.cfg.Store,
		wc.broker,
		&wsSubagentEvents{wc: wc},
		wc.server.cfg.SubagentSystemPrompt,
		sessionID,
	)
	perRunReg := wc.server.cfg.Registry.CloneWith(spawnTool)
	loop := agent.NewLoop(wc.server.cfg.LLMClient, perRunReg, wc.server.cfg.Store, wc.server.cfg.SystemPrompt)

	onThinking := func(token string) {
		wc.send(outMsg{Type: "thinking_token", Data: token})
	}

	err := loop.Run(ctx, sessionID, wc.tabID, content, wc.broker,
		func(token string) { wc.send(outMsg{Type: "token", Data: token}) },
		func(errMsg string) { wc.send(outMsg{Type: "error", Message: errMsg}) },
		func(tool, params, result string, approved bool) {
			wc.send(outMsg{
				Type:     "tool_result",
				Tool:     tool,
				Params:   json.RawMessage(params),
				Result:   result,
				Approved: approved,
			})
		},
		steerCh,
		func(prompt, completion, total int) {
			wc.send(outMsg{
				Type:             "usage",
				PromptTokens:     prompt,
				CompletionTokens: completion,
				ContextWindow:    contextWindow,
			})
		},
		onThinking,
	)

	if err != nil {
		if ctx.Err() == nil {
			wc.send(outMsg{Type: "error", Message: err.Error()})
		} else {
			wc.send(outMsg{Type: "stopped"})
		}
		return
	}
	if ctx.Err() != nil {
		wc.send(outMsg{Type: "stopped"})
	}
	wc.send(outMsg{Type: "done", MessageID: ""})

	if isNew && wc.server.cfg.LLMClient != nil {
		go func() {
			tCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			titleLoop := agent.NewLoop(wc.server.cfg.LLMClient, wc.server.cfg.Registry, wc.server.cfg.Store, "")
			title, err := titleLoop.GenerateTitle(tCtx, content)
			if err != nil {
				log.Printf("GenerateTitle: %v", err)
				return
			}
			if err := wc.server.cfg.Store.UpdateSessionTitle(tCtx, sessionID, title); err != nil {
				log.Printf("UpdateSessionTitle: %v", err)
				return
			}
			sessions, _ := wc.server.cfg.Store.ListSessions(tCtx)
			if sessions == nil {
				sessions = []session.Session{}
			}
			wc.send(outMsg{Type: "sessions", Data: sessions})
		}()
	}
	if !isNew {
		sessions, _ := wc.server.cfg.Store.ListSessions(ctx)
		wc.send(outMsg{Type: "sessions", Data: sessions})
	}
}
```

**Step 5: Update main.go**

Remove the `loop` variable creation. Update the `server.New(...)` call:

```go
// Remove this line:
// loop := agent.NewLoop(llmClient, reg, store, cfg.SystemPrompt)

srv := server.New(server.Config{
    Store:                store,
    LLMClient:            llmClient,
    Registry:             reg,
    SystemPrompt:         cfg.SystemPrompt,
    SubagentSystemPrompt: cfg.SubagentSystemPrompt,
    Approval:             cfg.Approval,
    FullConfig:           cfg,
    ConfigPath:           *configPath,
    ContextWindow:        cfg.Endpoint.ContextWindow,
})
```

Also remove the import of `"github.com/jhyoong/gohome/internal/agent"` from main.go if it is now unused (the loop is no longer created there). Verify with `go build`.

**Step 6: Fix server_test.go**

The test references `server.Config` without `Loop`. After the refactor it compiles as-is since the test only needs `Store`. Verify:

```bash
go test ./internal/server/... -v
```

Expected: all PASS.

**Step 7: Full build and test**

```bash
go build ./...
go test ./...
```

Expected: all PASS.

**Step 8: Commit**

```bash
git add internal/server/server.go cmd/agent/main.go internal/server/server_test.go
git commit -m "feat(server): wire SpawnSubagentTool with per-run registry and refactor Config"
```

---

### Task 6: Frontend — handle subagent events and render collapsible blocks

**Files:**
- Modify: `web/static/app.js`
- Modify: `web/static/app.css`

**Step 1: Add subagent block tracking and helper functions to app.js**

After the `let streamingThinkingEl = null;` line, add:

```js
// Maps subagent session_id -> { blockEl, bodyEl, tokenEl, thinkingEl }
const subagentBlocks = new Map();
```

Add these functions before the `// ---- Boot ----` comment:

```js
// ---- Subagent rendering ----

function openSubagentBlock(sessionID, parentID) {
  const blockEl = document.createElement('div');
  blockEl.className = 'subagent-block';
  blockEl.dataset.sessionId = sessionID;

  blockEl.innerHTML = `
    <button class="subagent-header" data-subagent-toggle>
      <span class="subagent-status running">◉</span>
      <span class="subagent-label">Subagent</span>
      <span class="subagent-toggle">▼</span>
    </button>
    <div class="subagent-body"></div>
  `;

  blockEl.querySelector('[data-subagent-toggle]').addEventListener('click', () => {
    const body = blockEl.querySelector('.subagent-body');
    const toggle = blockEl.querySelector('.subagent-toggle');
    const hidden = body.hidden;
    body.hidden = !hidden;
    toggle.textContent = hidden ? '▼' : '▶';
  });

  dom.messages.appendChild(blockEl);
  scrollToBottom();

  subagentBlocks.set(sessionID, {
    blockEl,
    bodyEl: blockEl.querySelector('.subagent-body'),
    tokenEl: null,
    thinkingEl: null,
  });
}

function appendSubagentToken(sessionID, token) {
  const entry = subagentBlocks.get(sessionID);
  if (!entry) return;
  if (!entry.tokenEl) {
    entry.tokenEl = document.createElement('div');
    entry.tokenEl.className = 'subagent-text';
    entry.bodyEl.appendChild(entry.tokenEl);
  }
  entry.tokenEl.textContent += token;
  scrollToBottom();
}

function appendSubagentThinkingToken(sessionID, token) {
  const entry = subagentBlocks.get(sessionID);
  if (!entry) return;
  if (!entry.thinkingEl) {
    const wrapper = document.createElement('div');
    wrapper.innerHTML = thinkingBlockHtml('');
    entry.bodyEl.insertBefore(wrapper.firstElementChild, entry.bodyEl.firstChild);
    entry.thinkingEl = entry.bodyEl.querySelector('.thinking-body');
  }
  entry.thinkingEl.textContent += token;
  scrollToBottom();
}

function addSubagentToolResult(msg) {
  const entry = subagentBlocks.get(msg.session_id);
  if (!entry) return;
  const tr = {
    tool_name: msg.tool,
    params: msg.params,
    result: msg.result,
    approved: msg.approved,
  };
  const wrapper = document.createElement('div');
  wrapper.innerHTML = toolCallBlockHtml(tr);
  const toolBlock = wrapper.firstElementChild;
  toolBlock.querySelector('[data-tool-toggle]').addEventListener('click', function() {
    const body = toolBlock.querySelector('.tool-call-body');
    const toggle = toolBlock.querySelector('.tool-call-toggle');
    body.hidden = !body.hidden;
    toggle.textContent = body.hidden ? '▼' : '▲';
  });
  entry.bodyEl.appendChild(toolBlock);
  scrollToBottom();
}

function finalizeSubagentBlock(sessionID) {
  const entry = subagentBlocks.get(sessionID);
  if (!entry) return;
  const status = entry.blockEl.querySelector('.subagent-status');
  status.textContent = '✓';
  status.className = 'subagent-status done';
  subagentBlocks.delete(sessionID);
}

function errorSubagentBlock(sessionID, errMsg) {
  const entry = subagentBlocks.get(sessionID);
  if (!entry) return;
  const status = entry.blockEl.querySelector('.subagent-status');
  status.textContent = '✗';
  status.className = 'subagent-status error';
  if (errMsg) {
    const errEl = document.createElement('div');
    errEl.className = 'subagent-error-text';
    errEl.textContent = errMsg;
    entry.bodyEl.appendChild(errEl);
  }
  subagentBlocks.delete(sessionID);
}
```

**Step 2: Add subagent cases to the ws.onmessage switch**

In the `switch (msg.type)` block, after `case 'usage':`, add:

```js
case 'subagent_start':         openSubagentBlock(msg.session_id, msg.data); break;
case 'subagent_token':         appendSubagentToken(msg.session_id, msg.data); break;
case 'subagent_thinking_token': appendSubagentThinkingToken(msg.session_id, msg.data); break;
case 'subagent_tool_result':   addSubagentToolResult(msg); break;
case 'subagent_done':          finalizeSubagentBlock(msg.session_id); break;
case 'subagent_error':         errorSubagentBlock(msg.session_id, msg.message); break;
```

**Step 3: Wire tool-toggle click handler for existing tool results**

Currently `toolCallBlockHtml` renders tool blocks as static HTML (no event listeners). The existing `data-tool-toggle` click handling is done via event delegation on `dom.messages`. Check if this is already set up in the codebase. If not, add event delegation in `initApp`:

```js
dom.messages.addEventListener('click', (e) => {
  const header = e.target.closest('[data-tool-toggle]');
  if (!header) return;
  const block = header.closest('.tool-call-block');
  if (!block) return;
  const body = block.querySelector('.tool-call-body');
  const toggle = block.querySelector('.tool-call-toggle');
  body.hidden = !body.hidden;
  toggle.textContent = body.hidden ? '▼' : '▲';
});
```

Note: check the existing app.js for this delegation before adding it — it may already exist.

**Step 4: Add CSS for subagent blocks**

At the end of `web/static/app.css`, add:

```css
/* Subagent blocks */
.subagent-block {
  margin-top: 8px;
  border: 1px solid var(--color-border);
  border-radius: 6px;
  overflow: hidden;
  font-size: 12px;
}

.subagent-header {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  background: var(--color-surface-alt);
  border: none;
  cursor: pointer;
  text-align: left;
}

.subagent-header:hover { background: var(--color-surface-hover); }

.subagent-label {
  flex: 1;
  font-weight: 600;
  font-family: monospace;
  text-transform: uppercase;
  color: var(--color-muted);
}

.subagent-toggle { color: var(--color-muted-mid); font-size: 10px; }

.subagent-status { font-weight: bold; }
.subagent-status.running { color: var(--color-muted); animation: pulse 1.5s infinite; }
.subagent-status.done { color: var(--color-approved); }
.subagent-status.error { color: var(--color-denied); }

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}

.subagent-body {
  padding: 8px 10px;
  background: var(--color-surface-faint);
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.subagent-text {
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.5;
}

.subagent-error-text {
  color: var(--color-denied);
  font-style: italic;
}
```

**Step 5: Build and verify**

```bash
go build ./...
go test ./...
```

Expected: all PASS.

**Step 6: Manual smoke test**

Run the server and open the browser. Configure a model that supports tool use. Ask the main agent to "spawn a subagent to list the files in the current directory". Verify:
- The subagent block appears with a pulsing indicator
- Tokens stream into it
- Tool approval modal appears for the shell command
- After approval, the tool result appears inside the subagent block
- The block finalizes with a ✓ indicator
- The main agent receives the subagent's response as a tool result

**Step 7: Commit**

```bash
git add web/static/app.js web/static/app.css
git commit -m "feat(frontend): render subagent blocks with streaming events"
```

---

### Task 7: Copy dist assets and final verification

The project embeds `web/static/` directly (no build step). `web/dist/` appears to be a copy. Check if dist needs updating:

```bash
diff web/static/app.js web/dist/app.js
diff web/static/app.css web/dist/app.css
```

If `web/dist/` is a stale copy that is not used at runtime (the binary embeds `web/static/` via `embed.go`), no action needed. If it is used, copy the files:

```bash
cp web/static/app.js web/dist/app.js
cp web/static/app.css web/dist/app.css
```

**Verify embed.go points to web/static, not web/dist:**

```bash
grep -n "web/" embed.go
```

Expected: `//go:embed web/static` or similar — confirming dist is not embedded.

**Final full test:**

```bash
go vet ./...
go test ./...
```

Expected: all PASS with no vet warnings.

**Commit:**

```bash
git add .
git commit -m "feat: subagents — delegate tasks to child sessions with streaming UI"
```
