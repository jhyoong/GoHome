# Remove Daemon Mode Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Restore the v0.2.5 in-process architecture while porting forward all non-daemon improvements from v0.3.x.

**Architecture:** Branch from `v0.2.5` tag (clean in-process agent-TUI with direct channels). Port forward visual overhaul, config overlay, in-process model switching, reasoning_effort support, and small fixes. No daemon, no RPC, no sockets.

**Tech Stack:** Go 1.25, Bubble Tea (charmbracelet/bubbletea), lipgloss

---

## Task 1: Create branch from v0.2.5 and verify base

**Files:**
- No file changes

**Step 1: Create the branch**

```bash
git checkout -b feature/remove-daemon v0.2.5
```

**Step 2: Verify it builds**

```bash
go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome
```

Expected: Binary compiles successfully.

**Step 3: Run tests**

```bash
go test ./gohome/...
```

Expected: All tests pass.

**Step 4: Commit (nothing to commit -- clean tag checkout)**

This is just verification. Proceed to Task 2.

---

## Task 2: Port agent package changes (events, turn stats, tool timing, reasoning_effort)

These are non-daemon improvements to the agent core: tool execution timing, per-turn stats, reasoning_effort support, ErrMessage field, removed unused EventToolCallStart, simplified AwaitUserInput signature, session.NewID, and small cleanups.

**Files:**
- Modify: `gohome/internal/agent/events.go`
- Modify: `gohome/internal/agent/turn.go`
- Modify: `gohome/internal/agent/run.go`
- Modify: `gohome/internal/agent/spawn.go`
- Modify: `gohome/internal/agent/agent.go`
- Modify: `gohome/internal/agent/state.go`
- Modify: `gohome/internal/llm/common/types.go`
- Modify: `gohome/internal/llm/openai/request.go`
- Modify: `gohome/internal/llm/openai/translate.go`
- Modify: `gohome/internal/tui/frontend.go`
- Test: `gohome/internal/agent/events_test.go`
- Test: `gohome/internal/agent/turn_test.go`
- Test: `gohome/internal/agent/run_test.go`

**Step 1: Port events.go changes**

Apply the diff from `git diff v0.2.5..HEAD -- gohome/internal/agent/events.go`. Key changes:
- Remove `EventToolCallStart` constant
- Add `Duration` field to `ToolResult` struct (type `time.Duration`)
- Add `TurnStats` struct with `OutputTokens`, `InputTokens`, `CacheReadTokens`, `CacheWriteTokens`, `Elapsed`
- Add `TurnStats *TurnStats`, `ErrMessage string`, `Duration time.Duration` fields to `Event`
- Add JSON tags to `Event` and `ToolResult` (these are useful for session persistence even without daemon)
- Change `AwaitUserInput` signature: remove the `sessionID string` parameter so it becomes `AwaitUserInput(ctx context.Context) (string, error)`

IMPORTANT: Do NOT add `json:"-"` tag to `Err` field and do NOT add `ErrMessage` field. These were only needed for JSON-RPC wire serialization. Keep `Err error` as-is without JSON tags since we won't serialize Events over RPC.

Actually, do add JSON tags since the Event struct is used in session JSONL persistence too. Keep `Err error` with `json:"-"` and add `ErrMessage string` with `json:"errMessage,omitempty"` -- this is still useful for error logging.

**Step 2: Port common/types.go**

Add `ReasoningEffort string` field to `common.Request`.

**Step 3: Port openai/request.go**

Add `ReasoningEffort string` with `json:"reasoning_effort,omitempty"` to `openaiBody` struct. Set it in `buildOpenAIBody` from `req.ReasoningEffort`.

**Step 4: Port openai/translate.go**

Remove task-number references from comments (cosmetic only).

**Step 5: Port agent.go -- add ReasoningEffort, keep Frontend as direct field**

Add `ReasoningEffort string` field to the Agent struct. Do NOT add the mutex/SetFrontend/Frontend() method pattern from HEAD. Keep `Frontend Frontend` as a plain exported field (no mutex needed without daemon).

The v0.2.5 Agent struct should become:
```go
type Agent struct {
    Tools           *tools.Registry
    Guard           *guard.Guard
    Frontend        Frontend
    State           *SessionState
    System          string
    MaxTokens       int
    ThinkingBudget  int
    ReasoningEffort string
    Home            string
    subagentCounter atomic.Int32
}
```

**Step 6: Port turn.go -- add timing, reasoning_effort, and use Frontend directly**

Apply the diff but keep `a.Frontend.Emit(...)` (direct field access) instead of `a.Frontend().Emit(...)` (mutex getter). Key changes:
- Add `start := time.Now()` before `Stream` call
- Add `ReasoningEffort: a.ReasoningEffort` to the Request
- Add `ErrMessage: ev.Err.Error()` to error event
- Build `TurnStats` from usage and elapsed time after the turn
- Include `TurnStats` in `EventTurnDone` event

**Step 7: Port run.go -- add tool timing, propagate errors**

Apply the diff but keep `a.Frontend.Emit(...)`. Key changes:
- `dispatchTool` returns `(content string, isError bool, elapsed time.Duration)` instead of `(content, isError)`
- Add `start := time.Now()` and `elapsed = time.Since(start)` around `safeExecute`
- Include `Duration: elapsed` in `ToolResult` within `EventToolResult`
- Propagate `execErr` from `safeExecute` instead of discarding it
- Remove `_ = stopReason` line

**Step 8: Port spawn.go -- add ReasoningEffort, keep Frontend direct**

Add `ReasoningEffort: a.ReasoningEffort` to child agent construction. Keep `Frontend: a.Frontend` (direct assignment, not mutex getter). Keep `a.Frontend.Emit(...)` calls.

**Step 9: Port state.go -- simplify Swap return**

Change `Swap` to return just `error` instead of `(queued bool, err error)`. The `queued` return was unused after daemon cleanup.

**Step 10: Port frontend.go (tui) -- update AwaitUserInput signature**

Remove the `sessionID string` parameter from `AwaitUserInput`:
```go
func (f *Frontend) AwaitUserInput(ctx context.Context) (string, error) {
```
Remove the `_ string` parameter that was there before.

**Step 11: Run tests**

```bash
go test ./gohome/internal/agent/... ./gohome/internal/llm/... ./gohome/internal/tui/...
```

Expected: All existing tests pass. Some may need signature updates for `AwaitUserInput` and `Swap`.

**Step 12: Commit**

```bash
git add gohome/internal/agent/ gohome/internal/llm/ gohome/internal/tui/frontend.go
git commit -m "feat: port agent improvements from v0.3.x (turn stats, tool timing, reasoning_effort)"
```

---

## Task 3: Port session package changes

Small fixes to session persistence code.

**Files:**
- Modify: `gohome/internal/session/session.go`
- Modify: `gohome/internal/session/events.go`
- Modify: `gohome/internal/session/list.go`
- Modify: `gohome/internal/session/load.go`
- Modify: `gohome/internal/session/validate.go`
- Modify: `gohome/internal/session/writer.go`
- Test: `gohome/internal/session/events_test.go`
- Test: `gohome/internal/session/list_test.go`

**Step 1: Add session.NewID()**

Add `NewID()` function to `session/session.go` (moved from `main.go`'s `newSessionID`).

**Step 2: Unexport internal functions**

- `Encode` -> `encode` in `events.go`
- `ValidateHistory` -> `validateHistory` in `validate.go`
- `IsBlank` -> `isBlank` in `list.go`
- `CleanBlank` -> `cleanBlank` in `list.go`
- Update call sites in `writer.go` (`encode`), `load.go` (`validateHistory`), and `list.go` internal calls

**Step 3: Add JSON tags to Listing**

Add JSON tags to `Listing` struct fields in `list.go` (useful for any future serialization).

**Step 4: Add empty-ID rejection in parseListing**

After the main scan loop in `parseListing`, add:
```go
if listing.ID == "" {
    return Listing{}, fmt.Errorf("no session_start found in %s", path)
}
```

**Step 5: Remove ToolUseID from SubagentSpawn/SubagentDone**

Remove the `ToolUseID` field from both structs in `events.go` (it was never set).

**Step 6: Simplify encode function**

Replace the marshal-unmarshal-into-map round-trip with the splice approach:
```go
ts := time.Now().UTC().Format(time.RFC3339)
header := fmt.Sprintf(`{"type":%q,"ts":%q`, typeName, ts)
if len(raw) > 2 {
    return append([]byte(header+","), raw[1:]...), nil
}
return []byte(header + "}"), nil
```

**Step 7: Update tests**

Update `events_test.go` to call `encode` (unexported -- tests are in same package so this works). Update any tests referencing removed fields.

**Step 8: Run tests**

```bash
go test ./gohome/internal/session/...
```

Expected: All tests pass.

**Step 9: Commit**

```bash
git add gohome/internal/session/
git commit -m "refactor: port session package cleanups from v0.3.x"
```

---

## Task 4: Port guard package changes

Small fix: replace `dirOf` with `filepath.Dir`, add `slog.Warn` for persist errors.

**Files:**
- Modify: `gohome/internal/guard/persist.go`
- Modify: `gohome/internal/guard/check.go`

**Step 1: Replace dirOf with filepath.Dir in persist.go**

Replace `dir := dirOf(w.projectPath)` with `dir := filepath.Dir(w.projectPath)`. Add `"path/filepath"` import. Delete the `dirOf` function.

**Step 2: Add slog.Warn in check.go**

Replace `_ = err` on whitelist persist failure with:
```go
slog.Warn("whitelist persist failed", "err", err)
```
Add `"log/slog"` import.

Do NOT add the mutex machinery (`sync.Mutex`, `SetFrontend` method). Those were daemon-only. Keep the guard's `frontend` field set once at construction.

**Step 3: Run tests**

```bash
go test ./gohome/internal/guard/...
```

**Step 4: Commit**

```bash
git add gohome/internal/guard/
git commit -m "fix: replace dirOf with filepath.Dir, log whitelist persist errors"
```

---

## Task 5: Port config package changes (reasoning_effort, annotated settings, skeleton)

**Files:**
- Modify: `gohome/internal/config/config.go`
- Create: `gohome/internal/config/skeleton.go`
- Create: `gohome/internal/config/skeleton_test.go`
- Test: `gohome/internal/config/config_test.go`

**Step 1: Add ReasoningEffort to ModelConfig**

Add `ReasoningEffort string` with `json:"reasoningEffort,omitempty"` to the `ModelConfig` struct.

**Step 2: Port AnnotatedSettings and LoadAnnotated**

Copy the `Source` type, constants (`SourceGlobal`, `SourceProject`, `SourceDefault`), `AnnotatedSettings` struct, and `LoadAnnotated` function from the HEAD version of `config.go`. These support the `/config` overlay.

**Step 3: Copy skeleton.go and skeleton_test.go**

Copy from HEAD:
```bash
git show HEAD:gohome/internal/config/skeleton.go > gohome/internal/config/skeleton.go
git show HEAD:gohome/internal/config/skeleton_test.go > gohome/internal/config/skeleton_test.go
```

These provide the setup wizard's config file generation. Review for any daemon-specific imports (there should be none -- skeleton is standalone).

**Step 4: Run tests**

```bash
go test ./gohome/internal/config/...
```

**Step 5: Commit**

```bash
git add gohome/internal/config/
git commit -m "feat: port config improvements (reasoning_effort, annotated settings, setup wizard skeleton)"
```

---

## Task 6: Port visual overhaul to TUI (PRs #21, #23)

This is the largest task. The visual overhaul touches many TUI files but is purely rendering logic with no daemon dependency.

**Strategy:** For each file, use `git diff v0.2.5..HEAD -- <file>` to identify changes, then apply them manually. Skip any lines referencing `cfe`, `ClientFrontend`, `rpc`, or daemon types.

**Files:**
- Modify: `gohome/internal/tui/chat.go`
- Modify: `gohome/internal/tui/model.go`
- Modify: `gohome/internal/tui/model_agent.go`
- Modify: `gohome/internal/tui/model_keys.go`
- Modify: `gohome/internal/tui/model_slash.go`
- Modify: `gohome/internal/tui/statusbar.go`
- Modify: `gohome/internal/tui/approval.go`
- Modify: `gohome/internal/tui/markdown.go`
- Modify: `gohome/internal/tui/help.go`
- Modify: `gohome/internal/tui/slash.go`
- Test: `gohome/internal/tui/tui_snapshot_test.go`

**Step 1: Port model.go structural changes**

Apply these changes to `Model` struct and related code, EXCLUDING daemon fields:
- Add `KindStats` constant
- Add `TurnStatsData` struct
- Add `Duration time.Duration` and `TurnStats *TurnStatsData` fields to `TimelineEntry`
- Add `configName string` field to `Model` (for status bar config display)
- Add `gitBranch string` and `projectDir string` fields to `Model`
- Remove `inputCh chan string` from Model struct -- WAIT, do NOT remove this. In v0.2.5's in-process model, `inputCh` is how user text reaches the agent. Keep it.
- Remove `homeDir string` and `cwd string` from Model struct (unused even in v0.2.5)
- Remove task-number comments
- Keep `New(fe *Frontend, sessionID string)` signature -- do NOT change to the daemon's single-arg signature

Add new setter methods:
```go
func (m *Model) SetConfigName(name string) { m.configName = name }
func (m *Model) SetGitBranch(branch string) { m.gitBranch = branch }
func (m *Model) SetProjectDir(dir string) { m.projectDir = dir }
```

Port the scroll/viewport improvements from the `View()` method and related rendering functions. These include gradient fades, scroll position tracking, and uniform entry spacing.

**Step 2: Port chat.go rendering changes**

This is the biggest file change (+185 lines). The changes add:
- Contextual tool call headers (bash shows `$ cmd`, read/write/edit show file paths)
- User message blocks with styled left border and background
- Tool call visual grouping with color-coded left borders (green/red/yellow)
- Per-tool execution timing display ("Took Xs")
- Turn stats rendering (tokens/sec, cache hits)
- Expand hints on truncated tool output
- Uniform blank-line separators
- Thinking content as dim italic (always inline, no collapse toggle)
- Whitespace-only text delta filtering

Use `git show HEAD:gohome/internal/tui/chat.go` as reference. The chat.go changes are entirely rendering logic with zero daemon dependency.

**Step 3: Port statusbar.go changes**

Add project directory, git branch display, and scroll position indicator. Add config name display alongside model name. Use `git diff v0.2.5..HEAD -- gohome/internal/tui/statusbar.go` as reference.

**Step 4: Port approval.go changes (PR #23)**

Streamline the approval overlay from verbose 4-line header to single contextual summary line. Use `git diff v0.2.5..HEAD -- gohome/internal/tui/approval.go` as reference.

**Step 5: Port markdown.go changes**

Add cyan heading styling and OSC 8 hyperlink support. Use `git diff v0.2.5..HEAD -- gohome/internal/tui/markdown.go` as reference.

**Step 6: Port model_agent.go changes**

Add turn stats handling and tool timing to agent event processing. The `handleAgentEvent` function needs to:
- Create `KindStats` timeline entries from `TurnStats` in `EventTurnDone`
- Store `Duration` from `ToolResult` into `TimelineEntry.Duration`
- Skip whitespace-only text deltas

Use `git diff v0.2.5..HEAD -- gohome/internal/tui/model_agent.go` as reference. Skip any changes that reference `cfe` or daemon types.

**Step 7: Port model_keys.go changes**

Add line-by-line arrow key scrolling within entries taller than viewport. Rebind session focus keys from `Ctrl+[/]` to `Ctrl+Left/Right`. Use `git diff v0.2.5..HEAD -- gohome/internal/tui/model_keys.go` as reference.

**Step 8: Port help.go changes**

Update keybinding documentation to reflect new key bindings. Use `git diff v0.2.5..HEAD -- gohome/internal/tui/help.go` as reference.

**Step 9: Port model_slash.go changes**

Update `handleSlashCommand` for:
- Add `/config` command handling (will be fully wired in Task 7)
- Update `/resume` to accept `(resolvedID string, history, error)` return from callback
- Add `m.configName = name` / `m.configName = configName` on model switch
- Port `historyToTimeline` if it changed

Update `SlashCallbacks` in `slash.go`:
- Change `ResumeSession` return to `func(id string) (string, []common.Message, error)`
- Add `OpenConfig func() (config.AnnotatedSettings, error)`

**Step 10: Run tests -- expect snapshot failures**

```bash
go test ./gohome/internal/tui/... 2>&1 | head -50
```

Expected: Snapshot tests fail because golden files need updating.

**Step 11: Update golden snapshot files**

```bash
go test ./gohome/internal/tui/ -run TestSnapshots -update
```

Then review the updated golden files to verify they look correct.

**Step 12: Run all TUI tests**

```bash
go test ./gohome/internal/tui/...
```

Expected: All tests pass with updated snapshots.

**Step 13: Commit**

```bash
git add gohome/internal/tui/
git commit -m "feat: port visual overhaul from v0.3.x (tool headers, user blocks, timing, scroll)"
```

---

## Task 7: Port config overlay, wizard, and editor TUI components

**Files:**
- Create: `gohome/internal/tui/config_overlay.go`
- Create: `gohome/internal/tui/config_wizard.go`
- Create: `gohome/internal/tui/config_editor.go`

**Step 1: Copy config TUI files from HEAD**

```bash
git show HEAD:gohome/internal/tui/config_overlay.go > gohome/internal/tui/config_overlay.go
git show HEAD:gohome/internal/tui/config_wizard.go > gohome/internal/tui/config_wizard.go
git show HEAD:gohome/internal/tui/config_editor.go > gohome/internal/tui/config_editor.go
```

**Step 2: Verify no daemon imports**

```bash
grep -n "daemon\|rpc\|client_frontend" gohome/internal/tui/config_overlay.go gohome/internal/tui/config_wizard.go gohome/internal/tui/config_editor.go
```

Expected: No matches. These files are standalone TUI components.

**Step 3: Verify compilation**

```bash
go build ./gohome/internal/tui/...
```

**Step 4: Run tests**

```bash
go test ./gohome/internal/tui/...
```

**Step 5: Commit**

```bash
git add gohome/internal/tui/config_overlay.go gohome/internal/tui/config_wizard.go gohome/internal/tui/config_editor.go
git commit -m "feat: port config overlay, wizard, and editor from v0.3.x"
```

---

## Task 8: Rewrite main.go for in-process architecture with all new features

This rewrites `main.go` to keep the v0.2.5 in-process architecture while adding the new features: `--config` flag, setup wizard, in-process model switching with param updates, config name in status bar, git branch detection, and session.NewID usage.

**Files:**
- Modify: `gohome/cmd/gohome/main.go`

**Step 1: Add --config flag**

Add to the flag definitions:
```go
showConfig = flag.Bool("config", false, "print merged configuration and exit")
```

Add handling after flag.Parse, before logging setup:
```go
if *showConfig {
    globalPath, _ := config.DefaultGlobalPath()
    projectPath := config.DefaultProjectPath(cwd)
    settings, err := config.Load(globalPath, projectPath)
    if err != nil {
        fmt.Fprintf(os.Stderr, "gohome: %v\n", err)
        os.Exit(1)
    }
    enc := json.NewEncoder(os.Stdout)
    enc.SetIndent("", "  ")
    enc.Encode(settings)
    os.Exit(0)
}
```

**Step 2: Replace newSessionID with session.NewID**

Replace all calls to `newSessionID()` with `session.NewID()`. Delete the local `newSessionID` function.

**Step 3: Wire setup wizard**

After config loading, if no model configs exist and stdin is a terminal, run the wizard:
```go
if len(settings.ModelConfig) == 0 && term.IsTerminal(int(os.Stdin.Fd())) {
    // Run setup wizard
    wizModel := tui.NewConfigWizard(globalPath)
    p := tea.NewProgram(wizModel)
    if _, err := p.Run(); err != nil {
        // wizard cancelled, continue with empty config
    }
    // Reload settings after wizard
    settings, _ = config.Load(globalPath, projectPath)
}
```

**Step 4: Wire git branch detection**

Add a helper to detect current git branch:
```go
func gitBranch(cwd string) string {
    cmd := exec.Command("git", "-C", cwd, "rev-parse", "--abbrev-ref", "HEAD")
    out, err := cmd.Output()
    if err != nil {
        return ""
    }
    return strings.TrimSpace(string(out))
}
```

Call it and set on model:
```go
m.SetGitBranch(gitBranch(cwd))
m.SetProjectDir(filepath.Base(cwd))
```

**Step 5: Wire in-process model switching**

Update the `SetModel` callback to also update agent params:
```go
SetModel: func(name string) (string, int, error) {
    mc, ok := settings.ModelConfig[name]
    if !ok {
        return "", 0, fmt.Errorf("unknown model config: %q", name)
    }
    apiKey := resolveAPIKey(mc)
    newClient, err := llm.New(mc.Wire, mc.BaseURL, apiKey, mc.ModelName, mc.Headers)
    if err != nil {
        return "", 0, fmt.Errorf("create client: %w", err)
    }
    state.SetClient(newClient)
    // Update agent params
    ag.MaxTokens = mc.MaxTokens
    ag.ThinkingBudget = mc.ThinkingBudget
    ag.ReasoningEffort = mc.ReasoningEffort
    return mc.ModelName, mc.ContextWindow, nil
},
```

**Step 6: Wire config overlay callback**

```go
OpenConfig: func() (config.AnnotatedSettings, error) {
    return config.LoadAnnotated(globalPath, projectPath)
},
```

**Step 7: Wire ReasoningEffort on agent construction**

When building the Agent, include ReasoningEffort:
```go
ag := &agent.Agent{
    // ... existing fields ...
    ReasoningEffort: selectedConfig.ReasoningEffort,
}
```

**Step 8: Update ResumeSession callback signature**

Change from `func(id string) ([]common.Message, error)` to `func(id string) (string, []common.Message, error)`. Return the resolved session ID as the first value.

**Step 9: Remove --stop flag handling (if any)**

The v0.2.5 base doesn't have `--stop`, so this is a no-op. Just verify there's no reference to daemon stopping.

**Step 10: Run build**

```bash
go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome
```

**Step 11: Run tests**

```bash
go test ./gohome/...
```

**Step 12: Commit**

```bash
git add gohome/cmd/gohome/main.go
git commit -m "feat: rewrite main.go with in-process model switching, config overlay, setup wizard"
```

---

## Task 9: Update and verify all tests

**Files:**
- Modify: Various test files that reference changed signatures
- Modify: `gohome/internal/tui/tui_snapshot_test.go`
- Modify: `gohome/cmd/gohome/main_test.go`

**Step 1: Fix compilation errors in tests**

Run `go build ./gohome/...` and fix any compilation errors caused by:
- `AwaitUserInput` signature change (remove sessionID param from test mocks)
- `Swap` return value change (single error instead of `(bool, error)`)
- `Frontend` field access vs method (keep as direct field)
- Renamed functions (`encode`, `validateHistory`, `isBlank`, `cleanBlank`)

**Step 2: Run all tests**

```bash
go test ./gohome/...
```

Fix any failures.

**Step 3: Update snapshot golden files if needed**

```bash
go test ./gohome/internal/tui/ -run TestSnapshots -update
```

**Step 4: Run full test suite again**

```bash
go test ./gohome/...
```

Expected: All tests pass.

**Step 5: Commit**

```bash
git add -A
git commit -m "test: fix all tests for in-process architecture"
```

---

## Task 10: Lint, vet, and final verification

**Files:**
- Possibly modify: any files flagged by linter

**Step 1: Run go vet**

```bash
go vet ./gohome/...
```

Expected: No issues.

**Step 2: Run golangci-lint**

```bash
golangci-lint run ./gohome/...
```

Fix any issues found.

**Step 3: Cross-build verification**

```bash
GOOS=linux GOARCH=amd64 go build -o /dev/null ./gohome/cmd/gohome
GOOS=darwin GOARCH=arm64 go build -o /dev/null ./gohome/cmd/gohome
GOOS=windows GOARCH=amd64 go build -o /dev/null ./gohome/cmd/gohome
```

Expected: All three compile. Windows should work again (no Unix socket dependency).

**Step 4: Check binary size**

```bash
go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome
ls -lh bin/gohome
```

Expected: Under 25 MB (CI guard threshold).

**Step 5: Manual smoke test**

```bash
./bin/gohome --version
./bin/gohome --config
```

Verify version prints and config output is valid JSON.

**Step 6: Commit any lint fixes**

```bash
git add -A
git commit -m "fix: resolve lint and vet issues"
```

---

## Task 11: Update documentation and changelog

**Files:**
- Modify: `README.md`
- Modify: `CHANGELOG.md`
- Modify: `CLAUDE.md`

**Step 1: Update README.md**

- Remove `--stop` flag from CLI flags table
- Remove `daemon.sock` from directory structure diagram (if present)
- Remove any mention of daemon mode
- Verify all other content is accurate

**Step 2: Update CHANGELOG.md**

Add v0.4.0 section at the top:

```markdown
## v0.4.0

### Changed

- **Architecture: restored in-process** -- Removed the daemon-client model introduced in v0.3.0. The agent now runs in the same process as the TUI with direct channel communication, eliminating Unix socket dependency, RPC serialization overhead, and associated concurrency bugs. Windows support is restored.
- **In-process model switching** -- /model command rebuilds the LLM client directly in-process instead of routing through daemon RPC. Agent params (maxTokens, thinkingBudget, reasoningEffort) are updated immediately.

### Removed

- **Daemon mode** -- Removed daemon server, JSON-RPC protocol, Unix socket communication, RPCFrontend, ClientFrontend, and --stop CLI flag.
- **--stop CLI flag** -- No daemon to stop.
```

**Step 3: Update CLAUDE.md**

Remove any references to daemon architecture. Update the architecture section to reflect in-process model.

**Step 4: Commit**

```bash
git add README.md CHANGELOG.md CLAUDE.md
git commit -m "docs: update documentation for v0.4.0 (daemon removal)"
```

---

## Task 12: Clean up daemon artifacts and dead code

**Files:**
- Delete: `gohome/internal/daemon/` (entire directory -- should not exist on this branch, but verify)
- Delete: `gohome/internal/tui/client_frontend.go` (should not exist)
- Delete: `gohome/internal/tui/agent_msgs.go` (should not exist -- v0.2.5 has frontend.go instead)
- Delete: Any design docs or plan files related to daemon from `docs/`

**Step 1: Verify no daemon code exists**

Since we branched from v0.2.5, daemon code should not be present. Verify:
```bash
find gohome/ -name "*daemon*" -o -name "*client_frontend*"
grep -r "daemon\|ClientFrontend\|RPCFrontend\|unix socket" gohome/ --include="*.go" -l
```

Expected: No matches (other than possibly test comments or changelog).

**Step 2: Check for dead code**

```bash
grep -rn "dirOf\|EventToolCallStart\|newSessionID" gohome/ --include="*.go"
```

Remove any remaining references to deleted functions.

**Step 3: Commit if any cleanup was needed**

```bash
git add -A
git commit -m "chore: remove dead code and daemon artifacts"
```

---

## Summary

| Task | Description | Risk |
|------|-------------|------|
| 1 | Branch from v0.2.5, verify | None |
| 2 | Port agent changes (timing, stats, reasoning_effort) | Low |
| 3 | Port session cleanups | Low |
| 4 | Port guard fixes | Low |
| 5 | Port config changes (reasoning_effort, annotated settings, skeleton) | Low |
| 6 | Port visual overhaul (biggest task) | Medium |
| 7 | Copy config overlay/wizard TUI components | Low |
| 8 | Rewrite main.go with in-process features | Medium |
| 9 | Fix and verify all tests | Medium |
| 10 | Lint, vet, cross-build | Low |
| 11 | Update docs and changelog | Low |
| 12 | Final cleanup | Low |
