# Context Window Tracking Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Display live token usage vs. max context window below the chat input bar as `50k / 131k (38%)`, sourced from `stream_options: {include_usage: true}` in the LLM streaming API.

**Architecture:** Add an `onUsage` callback to `llm.Stream()` that fires when the API's final usage SSE chunk arrives. The callback flows up through `agent.Loop.Run()` and `server.runAgent()`, which emits a `usage` WebSocket message. The frontend handles that message and updates a DOM element below the input bar.

**Tech Stack:** Go 1.21+, vanilla JS, SQLite (no changes). Run tests with `go test ./...` from the repo root.

---

### Task 1: Config — add `context_window` field with default

**Files:**
- Modify: `internal/config/config.go:12-18`
- Modify: `internal/config/config_test.go`

**Step 1: Write the failing test**

Add to `internal/config/config_test.go` after `TestSaveAndReload`:

```go
func TestContextWindowDefault(t *testing.T) {
	yaml := `
endpoint:
  url: "http://localhost:8080/v1"
  model: "my-model"
`
	f, err := os.CreateTemp("", "config*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(yaml)
	f.Close()

	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Endpoint.ContextWindow != 131072 {
		t.Errorf("got ContextWindow %d, want 131072", cfg.Endpoint.ContextWindow)
	}
}

func TestContextWindowExplicit(t *testing.T) {
	yaml := `
endpoint:
  url: "http://localhost:8080/v1"
  model: "my-model"
  context_window: 65536
`
	f, err := os.CreateTemp("", "config*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(yaml)
	f.Close()

	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Endpoint.ContextWindow != 65536 {
		t.Errorf("got ContextWindow %d, want 65536", cfg.Endpoint.ContextWindow)
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/config/... -run TestContextWindow -v
```

Expected: FAIL — `cfg.Endpoint.ContextWindow` field does not exist.

**Step 3: Add the field to `EndpointConfig`**

In `internal/config/config.go`, change:

```go
type EndpointConfig struct {
	URL         string  `yaml:"url"`
	APIKey      string  `yaml:"api_key"`
	Model       string  `yaml:"model"`
	MaxTokens   int     `yaml:"max_tokens"`
	Temperature float64 `yaml:"temperature"`
	ContextWindow int   `yaml:"context_window"`
}
```

Then in `Load()`, after the existing defaults block (after `cfg.Server.Port == 0` check), add:

```go
if cfg.Endpoint.ContextWindow == 0 {
    cfg.Endpoint.ContextWindow = 131072
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/config/... -v
```

Expected: PASS — all config tests including the two new ones.

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: add context_window field to EndpointConfig, default 131072"
```

---

### Task 2: LLM Client — `stream_options` and `onUsage` callback

**Files:**
- Modify: `internal/llm/client.go`
- Modify: `internal/llm/client_test.go`

**Step 1: Write the failing test**

Add `TestStreamingUsage` to `internal/llm/client_test.go`:

```go
func TestStreamingUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify stream_options was sent
		var body struct {
			StreamOptions *struct {
				IncludeUsage bool `json:"include_usage"`
			} `json:"stream_options"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.StreamOptions == nil || !body.StreamOptions.IncludeUsage {
			t.Error("stream_options.include_usage not set in request")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		// Usage chunk: empty choices, usage field present
		w.Write([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"})
	var gotPrompt, gotCompletion, gotTotal int
	err := client.Stream(context.Background(), []llm.Message{{Role: "user", Content: "hello"}}, nil,
		func(token string) {},
		func(_ []llm.ToolCall) {},
		func() {},
		func(prompt, completion, total int) {
			gotPrompt = prompt
			gotCompletion = completion
			gotTotal = total
		},
	)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if gotPrompt != 10 || gotCompletion != 5 || gotTotal != 15 {
		t.Errorf("usage: got (%d, %d, %d), want (10, 5, 15)", gotPrompt, gotCompletion, gotTotal)
	}
}
```

Also update the existing `TestStreamingTokens` call to add `nil` as the 7th argument (the new `onUsage` parameter):

```go
err := client.Stream(context.Background(), []llm.Message{{Role: "user", Content: "hello"}}, nil,
    func(token string) { tokens = append(tokens, token) },
    func(_ []llm.ToolCall) {},
    func() { doneCalled = true },
    nil, // onUsage
)
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/llm/... -run TestStreamingUsage -v
```

Expected: FAIL — `Stream` does not accept 7 arguments.

**Step 3: Update `internal/llm/client.go`**

Add the `streamOptions` type and update `reqBody`:

```go
type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type reqBody struct {
	Model         string         `json:"model"`
	Messages      []Message      `json:"messages"`
	Tools         []interface{}  `json:"tools,omitempty"`
	Stream        bool           `json:"stream"`
	MaxTokens     int            `json:"max_tokens,omitempty"`
	Temperature   float64        `json:"temperature,omitempty"`
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
}
```

Update the `Stream()` signature and body. Set `StreamOptions` when building the request body, and parse the usage chunk in the scanner loop:

```go
func (c *Client) Stream(ctx context.Context, messages []Message, tools []interface{},
	onToken func(string), onToolCalls func([]ToolCall), onDone func(),
	onUsage func(promptTokens, completionTokens, totalTokens int),
) error {

	body := reqBody{
		Model: c.cfg.Model, Messages: messages, Tools: tools,
		Stream: true, MaxTokens: c.cfg.MaxTokens, Temperature: c.cfg.Temperature,
		StreamOptions: &streamOptions{IncludeUsage: true},
	}
	// ... rest of setup unchanged ...
```

In the scanner loop, add usage chunk detection. The usage chunk has empty `choices` and a non-nil `usage` field. Extend the chunk struct in the scanner:

```go
var chunk struct {
    Choices []struct {
        Delta struct {
            Content   string `json:"content"`
            ToolCalls []struct {
                Index    int    `json:"index"`
                ID       string `json:"id"`
                Type     string `json:"type"`
                Function struct {
                    Name      string `json:"name"`
                    Arguments string `json:"arguments"`
                } `json:"function"`
            } `json:"tool_calls"`
        } `json:"delta"`
        FinishReason *string `json:"finish_reason"`
    } `json:"choices"`
    Usage *struct {
        PromptTokens     int `json:"prompt_tokens"`
        CompletionTokens int `json:"completion_tokens"`
        TotalTokens      int `json:"total_tokens"`
    } `json:"usage"`
}
```

After parsing the chunk, add before the existing `if len(chunk.Choices) == 0` guard:

```go
if len(chunk.Choices) == 0 && chunk.Usage != nil && onUsage != nil {
    onUsage(chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens, chunk.Usage.TotalTokens)
    continue
}
if len(chunk.Choices) == 0 {
    continue
}
```

**Step 4: Run all LLM tests**

```bash
go test ./internal/llm/... -v
```

Expected: PASS — both `TestStreamingTokens` and `TestStreamingUsage` pass.

**Step 5: Commit**

```bash
git add internal/llm/client.go internal/llm/client_test.go
git commit -m "feat: add stream_options include_usage and onUsage callback to Stream()"
```

---

### Task 3: Agent Loop — forward `onUsage` through `Run()`

**Files:**
- Modify: `internal/agent/loop.go`
- Modify: `internal/agent/loop_test.go`

**Step 1: Write the failing test**

Add `TestLoopUsageForwarded` to `internal/agent/loop_test.go`. Also add a helper `sseTextWithUsage`:

```go
func sseTextWithUsage(content string, prompt, completion, total int) string {
	usageJSON := fmt.Sprintf(`{"prompt_tokens":%d,"completion_tokens":%d,"total_tokens":%d}`, prompt, completion, total)
	return strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"` + content + `"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: {"choices":[],"usage":` + usageJSON + `}`,
		`data: [DONE]`,
		"",
	}, "\n\n")
}

func TestLoopUsageForwarded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseTextWithUsage("hello", 20, 3, 23)))
	}))
	defer srv.Close()

	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()
	sess, _ := store.CreateSession(ctx)

	loop := agent.NewLoop(llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"}), tools.NewRegistry(), store, "")
	broker := approval.NewBroker(config.ApprovalConfig{}, nil)

	var gotPrompt, gotTotal int
	err := loop.Run(ctx, sess.ID, "tab-1", "hello", broker,
		func(tok string) {},
		func(msg string) {},
		nil, // onToolResult
		nil, // steerCh
		func(prompt, completion, total int) {
			gotPrompt = prompt
			gotTotal = total
		},
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotPrompt != 20 || gotTotal != 23 {
		t.Errorf("usage: got prompt=%d total=%d, want prompt=20 total=23", gotPrompt, gotTotal)
	}
}
```

Also add `"fmt"` to the imports in `loop_test.go` (it's needed for `sseTextWithUsage`).

Update all existing `loop.Run()` calls in `loop_test.go` to add `nil` as the last argument (the new `onUsage` parameter). The calls are at lines 69, 116, 261. Each becomes:

```go
err := loop.Run(ctx, sess.ID, "tab-1", "hello", broker,
    func(tok string) { tokens = append(tokens, tok) },
    func(msg string) {},
    nil, // onToolResult
    nil, // steerCh
    nil, // onUsage
)
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/agent/... -run TestLoopUsageForwarded -v
```

Expected: FAIL — `Run` does not accept the `onUsage` argument.

**Step 3: Update `internal/agent/loop.go`**

Change the `Run()` signature:

```go
func (l *Loop) Run(ctx context.Context, sessionID, tabID, userMessage string,
	broker *approval.Broker,
	onToken func(string),
	onError func(string),
	onToolResult func(tool, params, result string, approved bool),
	steerCh <-chan string,
	onUsage func(prompt, completion, total int),
) error {
```

Inside the loop, pass `onUsage` to each `l.llm.Stream()` call. There is one `Stream()` call in `Run()`. Change it from:

```go
err = l.llm.Stream(ctx, history, llmTools,
    tokenCollector,
    func(tcs []llm.ToolCall) { toolCalls = tcs; gotToolCalls = true },
    nil,
)
```

to:

```go
err = l.llm.Stream(ctx, history, llmTools,
    tokenCollector,
    func(tcs []llm.ToolCall) { toolCalls = tcs; gotToolCalls = true },
    nil,
    onUsage,
)
```

For `GenerateTitle`, it calls `l.llm.Stream()` with its own inline callbacks. Add `nil` as the `onUsage` argument there:

```go
err := l.llm.Stream(ctx, []llm.Message{...}, nil,
    func(token string) { sb.WriteString(token) },
    func(_ []llm.ToolCall) {},
    nil,
    nil, // onUsage not needed for title generation
)
```

**Step 4: Run all agent tests**

```bash
go test ./internal/agent/... -v
```

Expected: PASS — all existing tests plus `TestLoopUsageForwarded`.

**Step 5: Commit**

```bash
git add internal/agent/loop.go internal/agent/loop_test.go
git commit -m "feat: forward onUsage callback through Loop.Run()"
```

---

### Task 4: Server — emit `usage` WebSocket message

**Files:**
- Modify: `internal/server/server.go`
- Modify: `cmd/agent/main.go` (the call site that constructs `server.Config` and calls `loop.Run`)

**Step 1: Locate the `main.go` call site**

Read `cmd/agent/main.go` to find where `server.Config` is constructed and where `loop.Run` is called (it's called inside `server.go`, not main, but `server.Config` is built in main). Note the exact lines — you'll need to add `ContextWindow` to the `server.Config` literal.

**Step 2: Update `outMsg` and `server.Config` in `internal/server/server.go`**

Add `ContextWindow int` to `server.Config`:

```go
type Config struct {
	Store      *session.Store
	Loop       *agent.Loop
	Approval   config.ApprovalConfig
	FullConfig *config.Config
	ConfigPath string
	ContextWindow int // max context window in tokens
}
```

Add fields to `outMsg`:

```go
type outMsg struct {
	Type             string          `json:"type"`
	Data             any             `json:"data,omitempty"`
	RequestID        string          `json:"request_id,omitempty"`
	Tool             string          `json:"tool,omitempty"`
	Params           json.RawMessage `json:"params,omitempty"`
	Result           string          `json:"result,omitempty"`
	Approved         bool            `json:"approved,omitempty"`
	Message          string          `json:"message,omitempty"`
	MessageID        string          `json:"message_id,omitempty"`
	SessionID        string          `json:"session_id,omitempty"`
	Messages         any             `json:"messages,omitempty"`
	PromptTokens     int             `json:"prompt_tokens,omitempty"`
	CompletionTokens int             `json:"completion_tokens,omitempty"`
	ContextWindow    int             `json:"context_window,omitempty"`
	ping             bool
}
```

**Step 3: Update `runAgent` to pass `onUsage` to `loop.Run()`**

In `wsConn.runAgent`, the call to `wc.loop.Run(...)` needs a new last argument. Add an `onUsage` closure that sends the usage WebSocket message. The server needs access to `ContextWindow` — since `wsConn` has a reference to `wc.server`, access it via `wc.server.cfg.ContextWindow`.

Add `contextWindow int` as a field to `wsConn` (set when the wsConn is created in `handleWebSocket`), or access it directly via `wc.server.cfg.ContextWindow`.

The simplest approach: access it via `wc.server.cfg.ContextWindow` in the closure:

```go
err := wc.loop.Run(ctx, sessionID, wc.tabID, content, wc.broker,
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
            ContextWindow:    wc.server.cfg.ContextWindow,
        })
    },
)
```

**Step 4: Update `cmd/agent/main.go`**

Read the file, find where `server.Config{...}` is built, and add `ContextWindow: cfg.Endpoint.ContextWindow`.

**Step 5: Build to catch any compile errors**

```bash
go build ./...
```

Expected: no errors.

**Step 6: Run all tests**

```bash
go test ./...
```

Expected: PASS — all existing tests pass (server tests don't test WS deeply, so no new tests needed here; the integration is covered by the agent tests).

**Step 7: Commit**

```bash
git add internal/server/server.go cmd/agent/main.go
git commit -m "feat: emit usage WebSocket message with token counts and context window"
```

---

### Task 5: Frontend — display context usage below input bar

**Files:**
- Modify: `web/static/index.html`
- Modify: `web/static/app.js`
- Modify: `web/static/app.css`
- Modify: `web/dist/index.html` (mirror of static)
- Modify: `web/dist/app.js` (mirror of static)

Note: `web/dist/` mirrors `web/static/` — apply the same changes to both.

**Step 1: Add the DOM element to `web/static/index.html`**

Inside `.chat-view`, after `<form id="input-form" ...>`, add:

```html
<div id="context-usage" class="context-usage" hidden>
  <span id="context-usage-text"></span>
</div>
```

The updated `.chat-view` block becomes:

```html
<div class="chat-view">
  <div id="messages" class="messages"></div>
  <form id="input-form" class="input-bar">
    <input id="input" type="text" placeholder="Type a message..." aria-label="Message" />
    <button id="stop-btn" type="button" class="btn-stop" hidden>Stop</button>
    <button id="send-btn" type="submit" disabled>Send</button>
  </form>
  <div id="context-usage" class="context-usage" hidden>
    <span id="context-usage-text"></span>
  </div>
</div>
```

**Step 2: Add CSS to `web/static/app.css`**

Find the app.css file and add at the end:

```css
.context-usage {
  text-align: right;
  padding: 2px 8px 4px;
  font-size: 0.72rem;
  color: #888;
  user-select: none;
}
```

**Step 3: Update `web/static/app.js`**

Add `contextUsage` and `contextUsageText` to the `dom` refs object inside `DOMContentLoaded`:

```js
contextUsage:     document.getElementById('context-usage'),
contextUsageText: document.getElementById('context-usage-text'),
```

Add the `usage` case to `ws.onmessage`:

```js
case 'usage': updateContextUsage(msg); break;
```

Add the `updateContextUsage` function (near the other UI state functions):

```js
function updateContextUsage(msg) {
  const used = Math.round(msg.prompt_tokens / 1000);
  const max = Math.round(msg.context_window / 1000);
  const pct = Math.round((msg.prompt_tokens / msg.context_window) * 100);
  dom.contextUsageText.textContent = `${used}k / ${max}k (${pct}%)`;
  dom.contextUsage.hidden = false;
}
```

Reset the counter in two places:

In `dom.newChatBtn.addEventListener('click', ...)`, add:
```js
dom.contextUsage.hidden = true;
dom.contextUsageText.textContent = '';
```

In `onHistory()`, add:
```js
dom.contextUsage.hidden = true;
dom.contextUsageText.textContent = '';
```

**Step 4: Mirror changes to `web/dist/`**

Apply the identical changes to `web/dist/index.html`, `web/dist/app.js`, and `web/dist/app.css`.

**Step 5: Build and manual smoke test**

```bash
go build ./cmd/agent && ./agent --config ~/.gohome/config.yaml
```

Open the browser, send a message, and verify:
- The counter appears below the input bar after the first response
- It shows a format like `11 / 131k (0%)` or similar
- Starting a new chat hides the counter
- Loading a different session hides the counter

**Step 6: Commit**

```bash
git add web/static/index.html web/static/app.js web/static/app.css \
        web/dist/index.html web/dist/app.js web/dist/app.css
git commit -m "feat: show context window usage below input bar"
```

---

### Task 6: Final verification

**Step 1: Run full test suite**

```bash
go test ./... -v
```

Expected: all tests pass.

**Step 2: Build binary**

```bash
go build ./cmd/agent
```

Expected: no errors.

**Step 3: Commit (if any cleanup needed)**

If no changes are needed, no commit required.
