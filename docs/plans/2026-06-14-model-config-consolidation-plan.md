# Model Config Consolidation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Consolidate `/endpoint` and `/model` slash commands into a single `/model` command, renaming `endpoints` to `modelConfig` and `Endpoint` to `ModelConfig` throughout the codebase.

**Architecture:** Rename the config struct and all its references, merge the two slash command handlers into one that always does a full client rebuild, update tests to match.

**Tech Stack:** Go, Bubble Tea TUI, JSONL session persistence

---

### Task 1: Rename config.Endpoint to config.ModelConfig

**Files:**
- Modify: `gohome/internal/config/config.go:22-46` (struct + Settings + Load + ResolveAPIKey)
- Test: `gohome/internal/config/config_test.go`

**Step 1: Rename the struct and fields in config.go**

In `gohome/internal/config/config.go`, make these changes:

1. Rename `Endpoint` struct to `ModelConfig` (line 23).
2. Rename `Endpoint.DefaultModel` field to `ModelConfig.ModelName` with JSON tag `"modelName"` (line 28).
3. Rename `Settings.Endpoints` to `Settings.ModelConfig` with JSON tag `"modelConfig"` (line 37).
4. Rename `Settings.DefaultEndpoint` to `Settings.DefaultModel` with JSON tag `"defaultModel"` (line 38).
5. Update `ResolveAPIKey` parameter type from `Endpoint` to `ModelConfig` (line 125).
6. Update all references inside `Load()` function:
   - `make(map[string]Endpoint)` -> `make(map[string]ModelConfig)` (line 74)
   - `global.DefaultEndpoint` -> `global.DefaultModel` (line 75)
   - `global.Endpoints` -> `global.ModelConfig` (lines 84-85)
   - `project.Endpoints` -> `project.ModelConfig` (lines 87-88)
   - `project.DefaultEndpoint` -> `project.DefaultModel` (lines 91-92)
7. Update the comment on `ErrNoAPIKey` (line 11): change "endpoint" to "model config".
8. Update the comment on `Wire` (line 14): change "endpoint" to "model config".
9. Update the comment on `Endpoint` struct (line 22): now `ModelConfig`.

**Step 2: Update config_test.go**

In `gohome/internal/config/config_test.go`, update all references:

1. `TestSettings_ParseEndpoint` (line 13): rename to `TestSettings_ParseModelConfig`. Update JSON string to use `"modelConfig"` and `"modelName"` and `"defaultModel"` keys. Update assertions: `s.Endpoints["e1"]` -> `s.ModelConfig["e1"]`, `s.DefaultEndpoint` -> `s.DefaultModel`.
2. `TestLoad_MergesGlobalAndProject` (line 50): Update `Endpoints` -> `ModelConfig`, `Endpoint{...DefaultModel:...}` -> `ModelConfig{...ModelName:...}`, `DefaultEndpoint` -> `DefaultModel` everywhere in this test.
3. `TestLoad_ProjectDefaultEndpointWins` (line 100): rename to `TestLoad_ProjectDefaultModelWins`, update `DefaultEndpoint` -> `DefaultModel`.
4. `TestLoad_GlobalDefaultKeptWhenProjectEmpty` (line 118): update `DefaultEndpoint` -> `DefaultModel`.
5. `TestLoad_MalformedJSONTreatedAsEmpty` (line 144): update `DefaultEndpoint` -> `DefaultModel`.
6. `TestResolveAPIKey_*` tests (lines 164-201): change `Endpoint{...}` to `ModelConfig{...}`.
7. `TestLoad_EndpointMaxTokensAndThinkingBudget` (line 285): rename to `TestLoad_ModelConfigMaxTokensAndThinkingBudget`, update `Endpoints` -> `ModelConfig`, `Endpoint{...DefaultModel:...}` -> `ModelConfig{...ModelName:...}`, `DefaultEndpoint` -> `DefaultModel`, `merged.Endpoints["main"]` -> `merged.ModelConfig["main"]`.

**Step 3: Run tests**

Run: `go test ./gohome/internal/config/`
Expected: PASS

**Step 4: Commit**

```bash
git add gohome/internal/config/config.go gohome/internal/config/config_test.go
git commit -m "config: rename Endpoint -> ModelConfig, endpoints -> modelConfig"
```

---

### Task 2: Update LLM factory and wire clients

**Files:**
- Modify: `gohome/internal/llm/factory.go:16` (parameter type)
- Modify: `gohome/internal/llm/anthropic/client.go:28-32` (New function)
- Modify: `gohome/internal/llm/openai/client.go:26-30` (New function)
- Test: `gohome/internal/llm/factory_test.go`, `gohome/internal/llm/anthropic/client_test.go`, `gohome/internal/llm/openai/client_test.go`, `gohome/internal/llm/anthropic/retry_test.go`, `gohome/internal/llm/openai/retry_test.go`

**Step 1: Update factory.go**

In `gohome/internal/llm/factory.go`:
1. Change function signature: `func New(e config.Endpoint, apiKey string)` -> `func New(e config.ModelConfig, apiKey string)` (line 16).
2. Update doc comment (line 14): "endpoint's wire format" -> "model config's wire format".
3. Update package doc comment (line 2): "configured endpoint" -> "configured model config".

**Step 2: Update anthropic/client.go**

In `gohome/internal/llm/anthropic/client.go`:
1. Change `func New(e config.Endpoint, apiKey string)` -> `func New(e config.ModelConfig, apiKey string)` (line 28).
2. Change `e.DefaultModel` -> `e.ModelName` (line 32).
3. Update doc comment (line 27): "Endpoint" -> "ModelConfig".

**Step 3: Update openai/client.go**

In `gohome/internal/llm/openai/client.go`:
1. Change `func New(e config.Endpoint, apiKey string)` -> `func New(e config.ModelConfig, apiKey string)` (line 26).
2. Change `e.DefaultModel` -> `e.ModelName` (line 30).
3. Update doc comment (line 25): "Endpoint" -> "ModelConfig".

**Step 4: Update factory_test.go**

In `gohome/internal/llm/factory_test.go`:
1. Change all `config.Endpoint{...}` to `config.ModelConfig{...}` (lines 13, 23, 33).

**Step 5: Update anthropic/client_test.go**

In `gohome/internal/llm/anthropic/client_test.go`:
1. Change all `config.Endpoint{...}` to `config.ModelConfig{...}` (lines 35, 100).
2. Change `DefaultModel` -> `ModelName` in struct literals (lines 37, 102).

**Step 6: Update anthropic/retry_test.go**

In `gohome/internal/llm/anthropic/retry_test.go`:
1. Change all `config.Endpoint{...}` to `config.ModelConfig{...}` (lines 37, 74, 104).
2. Change `DefaultModel` -> `ModelName` in struct literals (lines 39, 76, 106).

**Step 7: Update openai/client_test.go**

In `gohome/internal/llm/openai/client_test.go`:
1. Change all `config.Endpoint{...}` to `config.ModelConfig{...}` (lines 41, 108, 136).
2. Change `DefaultModel` -> `ModelName` in struct literals (lines 43, 110, 138).

**Step 8: Update openai/retry_test.go**

In `gohome/internal/llm/openai/retry_test.go`:
1. Change all `config.Endpoint{...}` to `config.ModelConfig{...}` (lines 35, 71, 101).
2. Change `DefaultModel` -> `ModelName` in struct literals (lines 37, 73, 103).

**Step 9: Run tests**

Run: `go test ./gohome/internal/llm/...`
Expected: PASS

**Step 10: Commit**

```bash
git add gohome/internal/llm/
git commit -m "llm: update factory and clients to use ModelConfig"
```

---

### Task 3: Update session.Session and session events

**Files:**
- Modify: `gohome/internal/session/session.go:17,27` (Endpoint field + NewSession param)
- Modify: `gohome/internal/session/events.go:18` (SessionStart.Endpoint field)
- Modify: `gohome/internal/agent/spawn.go:39,65` (references to Session.Endpoint)

**Step 1: Rename Session.Endpoint to Session.ModelConfig**

In `gohome/internal/session/session.go`:
1. Change `Endpoint string` -> `ModelConfig string` (line 17).
2. Change the `endpoint` parameter name in `NewSession` to `modelConfig` and assign to `ModelConfig` (line 27-35).

**Step 2: Rename SessionStart.Endpoint to SessionStart.ModelConfig**

In `gohome/internal/session/events.go`:
1. Change `Endpoint string \`json:"endpoint"\`` -> `ModelConfig string \`json:"modelConfig"\`` (line 18).

**Step 3: Update agent/spawn.go**

In `gohome/internal/agent/spawn.go`:
1. Change `parent.Endpoint` -> `parent.ModelConfig` (line 39, in `NewSession` call).
2. Change `Endpoint: parent.Endpoint` -> `ModelConfig: parent.ModelConfig` (line 65, in `SessionStart` literal).

**Step 4: Run tests**

Run: `go test ./gohome/internal/session/ ./gohome/internal/agent/`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/session/ gohome/internal/agent/spawn.go
git commit -m "session: rename Endpoint -> ModelConfig in Session and events"
```

---

### Task 4: Update ModelSelectorComponent

**Files:**
- Modify: `gohome/internal/tui/model_selector.go:12-30,48` (Endpoint -> ModelConfig, DefaultModel -> ModelName)
- Test: `gohome/internal/tui/model_selector_test.go`

**Step 1: Update model_selector.go**

In `gohome/internal/tui/model_selector.go`:
1. Change `endpoints map[string]config.Endpoint` -> `configs map[string]config.ModelConfig` (line 12).
2. Change `func NewModelSelector(endpoints map[string]config.Endpoint, currentEndpoint string)` -> `func NewModelSelector(configs map[string]config.ModelConfig, current string)` (line 15).
3. Update all `endpoints` references inside `NewModelSelector` to `configs`, and `currentEndpoint` to `current` (lines 16-26).
4. Change `ep.DefaultModel` -> `ep.ModelName` (line 30).
5. Change `ms.endpoints` -> `ms.configs` in the constructor (line 43).
6. In `SetOnSelect`, change `ms.endpoints` -> `ms.configs` and `ep.DefaultModel` -> `ep.ModelName` (lines 49-51).

**Step 2: Update model_selector_test.go**

In `gohome/internal/tui/model_selector_test.go`:
1. Rename `sampleEndpoints()` to `sampleModelConfigs()` (line 11).
2. Change return type from `map[string]config.Endpoint` to `map[string]config.ModelConfig` (line 12).
3. Change `DefaultModel` -> `ModelName` in struct literals (lines 15, 19).
4. Update all calls from `sampleEndpoints()` to `sampleModelConfigs()` (lines 25, 37, 46, 56, 70).
5. Update test assertion strings from "endpoint" to "config" where they appear in test error messages (lines 32, 41, 50, 64).

**Step 3: Run tests**

Run: `go test ./gohome/internal/tui/ -run TestModelSelector`
Expected: PASS

**Step 4: Commit**

```bash
git add gohome/internal/tui/model_selector.go gohome/internal/tui/model_selector_test.go
git commit -m "tui: update ModelSelectorComponent to use ModelConfig"
```

---

### Task 5: Consolidate /model and /endpoint slash commands

**Files:**
- Modify: `gohome/internal/tui/slash.go:14-15` (remove SetEndpoint, change SetModel signature)
- Modify: `gohome/internal/tui/model_slash.go:17,125-189` (remove /endpoint, rewrite /model)

**Step 1: Update SlashCallbacks in slash.go**

In `gohome/internal/tui/slash.go`:
1. Remove the `SetEndpoint` field (line 15).
2. Change `SetModel func(name string) error` to `SetModel func(name string) (model string, contextWindow int, err error)` (line 14).

**Step 2: Rewrite /model handler and remove /endpoint in model_slash.go**

In `gohome/internal/tui/model_slash.go`:
1. Remove `"/endpoint"` from the `slashCommands` list (line 17). The list becomes:
   ```go
   var slashCommands = []string{
       "/help", "/new", "/resume", "/yolo", "/model", "/cancel", "/tokens", "/quit",
   }
   ```
2. Replace the entire `/model` case (lines 125-161) with new logic that does full client rebuild:
   ```go
   case "/model":
       if len(fields) >= 2 {
           name := fields[1]
           if m.slashCB.SetModel != nil {
               model, ctxWin, err := m.slashCB.SetModel(name)
               if err != nil {
                   m.statusMsg = fmt.Sprintf("/model: %v", err)
               } else {
                   m.modelName = model
                   if ctxWin > 0 {
                       m.contextWindow = ctxWin
                   }
                   m.settings.DefaultModel = name
                   m.statusMsg = "Model set to " + name
               }
           } else {
               m.statusMsg = "/model: not configured"
           }
           break
       }
       if len(m.settings.ModelConfig) == 0 {
           m.statusMsg = fmt.Sprintf("Current model: %s", m.modelName)
           break
       }
       ms := NewModelSelector(m.settings.ModelConfig, m.settings.DefaultModel)
       ms.SetOnSelect(func(configName, model string) {
           m.activeModal = nil
           if m.slashCB.SetModel == nil {
               m.statusMsg = "/model: not configured"
               return
           }
           model, ctxWin, err := m.slashCB.SetModel(configName)
           if err != nil {
               m.statusMsg = fmt.Sprintf("/model: %v", err)
               return
           }
           m.modelName = model
           if ctxWin > 0 {
               m.contextWindow = ctxWin
           }
           m.settings.DefaultModel = configName
           m.statusMsg = "Model set to " + configName
       })
       ms.SetOnCancel(func() {
           m.activeModal = nil
       })
       m.activeModal = ms
   ```
3. Remove the entire `/endpoint` case (lines 162-189).
4. Remove unused imports if any (`"os"` was already used by `/resume`; double-check `"fmt"` and `"strings"` are still used).

**Step 3: Run tests**

Run: `go test ./gohome/internal/tui/ -run TestSlash`
Expected: Compilation succeeds. Some tests may fail because they reference old callbacks -- those are updated in Task 6.

**Step 4: Commit**

```bash
git add gohome/internal/tui/slash.go gohome/internal/tui/model_slash.go
git commit -m "tui: consolidate /endpoint and /model into single /model command"
```

---

### Task 6: Update TUI tests for consolidated /model

**Files:**
- Modify: `gohome/internal/tui/slash_test.go:224-284` (endpoint tests -> model tests)
- Modify: `gohome/internal/tui/integration_test.go:179-184` (Settings struct literals)

**Step 1: Update slash_test.go**

In `gohome/internal/tui/slash_test.go`:

1. Rename `TestSlashEndpointNotConfigured` -> `TestSlashModelNoConfigs` (line 224). Change the typed command from `/endpoint` to `/model` (line 233). Change expected output from `"no endpoints configured"` to `"Current model:"` (line 237) — since the new `/model` with no args and no configs shows "Current model: ?".

2. Rename `TestSlashEndpointCallsCallback` -> `TestSlashModelCallsCallback` (line 241). Update the Settings literal:
   - `Endpoints` -> `ModelConfig` (line 244)
   - `config.Endpoint{DefaultModel: ...}` -> `config.ModelConfig{ModelName: ...}` (lines 245-246)
   - `DefaultEndpoint` -> `DefaultModel` (line 248)
   
   Update the callback:
   - `SetEndpoint: func(name string) (string, int, error)` -> `SetModel: func(name string) (string, int, error)` (line 253)
   
   Update the typed command from `/endpoint` to `/model` (line 266).
   Update the expected output from `"Endpoint set to"` to `"Model set to"` (line 278).
   Update the assertion message from `"SetEndpoint callback was not called"` to `"SetModel callback was not called"` (line 282).

**Step 2: Update integration_test.go**

In `gohome/internal/tui/integration_test.go`:
1. Change `Endpoints: map[string]config.Endpoint{` -> `ModelConfig: map[string]config.ModelConfig{` (line 180).
2. Change `DefaultModel` -> `ModelName` in the struct literal inside the map (line 181).
3. Change `DefaultEndpoint: "anthropic"` -> `DefaultModel: "anthropic"` (line 184).

**Step 3: Run tests**

Run: `go test ./gohome/internal/tui/`
Expected: PASS

**Step 4: Commit**

```bash
git add gohome/internal/tui/slash_test.go gohome/internal/tui/integration_test.go
git commit -m "tui: update tests for consolidated /model command"
```

---

### Task 7: Update main.go (CLI flags + callbacks + startup)

**Files:**
- Modify: `gohome/cmd/gohome/main.go:32-33,202-226,287-313,349-353,420-471`

**Step 1: Update CLI flags**

In `gohome/cmd/gohome/main.go`:
1. Remove `endpointName` flag (line 32). Replace with: rename the `--model` flag to select a named config, not a model string override.
2. Remove `modelName` flag (line 33).
3. Add a single flag: `modelFlag = flag.String("model", "", "model config name")` replacing both old flags.

The flags block becomes:
```go
var (
    modelFlag   = flag.String("model", "", "model config name override")
    yolo        = flag.Bool("yolo", false, "disable all approval prompts")
    resume      = flag.Bool("resume", false, "resume a past session")
    showVersion = flag.Bool("version", false, "print version and exit")
)
```

**Step 2: Update endpoint resolution block (lines 202-228)**

Replace the endpoint resolution + model override block with:
```go
// Resolve model config.
cfgName := *modelFlag
if cfgName == "" {
    cfgName = settings.DefaultModel
}
mc, ok := settings.ModelConfig[cfgName]
if !ok {
    if cfgName == "" {
        fmt.Fprintf(os.Stderr, "gohome: no model configured. Set defaultModel in ~/.gohome/settings.json or use --model.\n")
    } else {
        fmt.Fprintf(os.Stderr, "gohome: model config %q not found. Check ~/.gohome/settings.json.\n", cfgName)
    }
    os.Exit(1)
}

apiKey, err := config.ResolveAPIKey(mc)
if err != nil {
    fmt.Fprintf(os.Stderr, "gohome: no API key for model config %q.\n", cfgName)
    fmt.Fprintf(os.Stderr, "  Set apiKey in settings.json or the environment variable named by apiKeyEnv.\n")
    os.Exit(1)
}

// Build LLM client.
client, err := llm.New(mc, apiKey)
if err != nil {
    fmt.Fprintf(os.Stderr, "gohome: cannot create LLM client: %v\n", err)
    os.Exit(1)
}
```

**Step 3: Update session creation (lines 287-308)**

Replace all `endpoint.DefaultModel` references with `mc.ModelName`, and `epName` with `cfgName`:

1. `session.NewSession(newSessionID(), cwd, endpoint.DefaultModel, epName)` -> `session.NewSession(newSessionID(), cwd, mc.ModelName, cfgName)` (line 287).
2. In `SessionStart` literal: `Model: endpoint.DefaultModel` -> `Model: mc.ModelName`, `Endpoint: epName` -> `ModelConfig: cfgName` (lines 303-304).

**Step 4: Update TUI model setup (lines 312-318)**

1. `m.SetModelName(endpoint.DefaultModel)` -> `m.SetModelName(mc.ModelName)` (line 313).
2. `endpoint.ContextWindow` -> `mc.ContextWindow` (line 314).

**Step 5: Update maxTokens / thinkingBudget (lines 349-356)**

1. `endpoint.MaxTokens` -> `mc.MaxTokens` (line 349).
2. `endpoint.ThinkingBudget` -> `mc.ThinkingBudget` (line 353).

**Step 6: Update NewSession callback (lines 419-445)**

1. `endpoint.DefaultModel` -> `mc.ModelName` (lines 420, 435).
2. `epName` -> `cfgName` (lines 420, 436).
3. `Endpoint: epName` -> `ModelConfig: cfgName` (line 436).

**Step 7: Replace SetEndpoint + SetModel callbacks with single SetModel (lines 447-472)**

Remove the old `SetModel` and `SetEndpoint` callbacks. Replace with:
```go
SetModel: func(name string) (string, int, error) {
    cfg, ok := settings.ModelConfig[name]
    if !ok {
        return "", 0, fmt.Errorf("model config %q not found", name)
    }
    apiKey, err := config.ResolveAPIKey(cfg)
    if err != nil {
        return "", 0, fmt.Errorf("no API key for model config %q", name)
    }
    newClient, err := llm.New(cfg, apiKey)
    if err != nil {
        return "", 0, fmt.Errorf("create client for %q: %w", name, err)
    }
    state.SetClient(newClient)
    state.SetModel(cfg.ModelName)
    state.Session().ModelConfig = name
    ctxWin := cfg.ContextWindow
    if ctxWin <= 0 {
        ctxWin = config.DefaultContextWindow
    }
    return cfg.ModelName, ctxWin, nil
},
```

**Step 8: Build**

Run: `go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome`
Expected: Build succeeds

**Step 9: Commit**

```bash
git add gohome/cmd/gohome/main.go
git commit -m "main: rename --endpoint to --model, wire consolidated SetModel callback"
```

---

### Task 8: Update e2e tests

**Files:**
- Modify: `gohome/test/e2e/smoke_test.go:68-71`

**Step 1: Update smoke_test.go**

In `gohome/test/e2e/smoke_test.go`:
1. Change `config.Endpoint{...}` to `config.ModelConfig{...}` (line 68).
2. Change `DefaultModel: model` to `ModelName: model` (line 71).

**Step 2: Run vet**

Run: `go vet ./gohome/...`
Expected: PASS

**Step 3: Commit**

```bash
git add gohome/test/e2e/smoke_test.go
git commit -m "e2e: update smoke test to use ModelConfig"
```

---

### Task 9: Update README.md

**Files:**
- Modify: `README.md` (lines 20, 26, 33, 47-67, 83-95, 107, 154, 162, 168)

**Step 1: Update all endpoint references in README**

1. `--endpoint <name>` -> `--model <name>` in usage examples (lines 20, 26, 107).
2. `--endpoint local-anthropic --model claude-haiku-4-5` -> `--model local-anthropic` (line 26).
3. CLI flags table: remove `--endpoint` row, update `--model` row description to "Select a configured model config by name" (line 33).
4. Settings JSON example: `"endpoints"` -> `"modelConfig"`, `"defaultModel"` (field) -> `"modelName"`, `"defaultEndpoint"` -> `"defaultModel"` (lines 47-67).
5. Settings table: update field names and descriptions (lines 83-95).
6. Slash command table: remove `/endpoint` row (line 154).
7. Directory tree comments: update "endpoint config" / "endpoint/model overrides" (lines 162, 168).

**Step 2: Commit**

```bash
git add README.md
git commit -m "docs: update README for modelConfig naming"
```

---

### Task 10: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

**Step 1: Update any stale endpoint references**

Search for "endpoint" in `CLAUDE.md` and update:
1. `--endpoint` usage example -> `--model`.
2. Any references to `defaultEndpoint` in settings descriptions -> `defaultModel`.

**Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for modelConfig naming"
```

---

### Task 11: Full test suite and lint

**Step 1: Run all tests**

Run: `go test ./gohome/...`
Expected: PASS

**Step 2: Run vet**

Run: `go vet ./gohome/...`
Expected: PASS

**Step 3: Run lint**

Run: `golangci-lint run ./gohome/...`
Expected: PASS (or only pre-existing warnings)

**Step 4: Build**

Run: `go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome`
Expected: Build succeeds

**Step 5: Commit (if any fixups needed)**

```bash
git add -A
git commit -m "fix: address lint/vet issues from modelConfig rename"
```
