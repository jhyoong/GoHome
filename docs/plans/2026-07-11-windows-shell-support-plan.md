# Windows Shell Support Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Rename the "bash" tool to "shell" and use PowerShell on Windows instead of cmd /c, so the LLM generates native commands without wrapping in `powershell -Command`.

**Architecture:** Single "shell" tool with platform-aware backend. On Unix, delegates to `/bin/sh -c`. On Windows, delegates to `powershell.exe -NoProfile -NonInteractive -Command`. The tool name, description, config fields, guard/whitelist internals, TUI display, and golden snapshots all change from "bash" to "shell". Clean break, no backwards compat.

**Tech Stack:** Go 1.25, os/exec, runtime.GOOS

---

### Task 1: Rename tool file and struct (tools package)

**Files:**
- Rename: `gohome/internal/tools/bash.go` -> `gohome/internal/tools/shell.go`
- Rename: `gohome/internal/tools/bash_test.go` -> `gohome/internal/tools/shell_test.go`

**Step 1: Rename bash.go to shell.go via git mv**

Run:
```bash
git mv gohome/internal/tools/bash.go gohome/internal/tools/shell.go
git mv gohome/internal/tools/bash_test.go gohome/internal/tools/shell_test.go
```

**Step 2: Rename BashTool to ShellTool in shell.go**

In `gohome/internal/tools/shell.go`, make these changes:

- Rename struct `BashTool` to `ShellTool` (line 20-23)
- Rename all receiver references from `(b BashTool)` to `(s ShellTool)` and `b.` to `s.`
- Change `Name()` return from `"bash"` to `"shell"` (line 25)
- Make `Description()` platform-aware (line 27-33):

```go
func (s ShellTool) Description() string {
	if runtime.GOOS == "windows" {
		return "Execute a PowerShell command. " +
			"Default timeout is 120 000 ms; max is 600 000 ms. " +
			"stdout and stderr are merged. " +
			"Non-zero exit codes are reported as normal results, not errors. " +
			"Timeout or kill failures return IsError."
	}
	return "Execute a shell command. " +
		"Default timeout is 120 000 ms; max is 600 000 ms. " +
		"stdout and stderr are merged. " +
		"Non-zero exit codes are reported as normal results, not errors. " +
		"Timeout or kill failures return IsError."
}
```

- Rename `bashSchema` to `shellSchema` (line 35)
- Rename `bashInput` struct to `shellInput` (line 47)
- Change Windows exec (line 80-81) from `cmd /c` to PowerShell:

```go
if runtime.GOOS == "windows" {
	cmd = exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", inp.Command)
} else {
	cmd = exec.CommandContext(ctx, "/bin/sh", "-c", inp.Command)
}
```

- Rename all error prefix strings from `"bash: "` to `"shell: "` (lines 57, 122-128, 135-140, 149)

**Step 3: Update shell_test.go**

- Change `&BashTool{}` to `&ShellTool{}` (lines 18, 105)
- All test logic stays the same -- the unix tests still skip on Windows via `runtime.GOOS == "windows"`

**Step 4: Run tests to verify**

Run: `go test ./gohome/internal/tools/ -v -run TestBash`
Expected: All tests PASS (test function names still say "Bash" which is fine)

**Step 5: Commit**

```bash
git add gohome/internal/tools/shell.go gohome/internal/tools/shell_test.go
git commit -m "refactor: rename BashTool to ShellTool, use PowerShell on Windows"
```

---

### Task 2: Rename config fields (config package)

**Files:**
- Modify: `gohome/internal/config/defaults.go:9-10`
- Modify: `gohome/internal/config/config.go:41-42,79-80,101-106,187-220`
- Modify: `gohome/internal/config/skeleton.go:29-30`
- Modify: `gohome/internal/config/config_test.go` (lines with BashTimeoutMs)
- Modify: `gohome/internal/config/skeleton_test.go:27`

**Step 1: Update defaults.go**

Change:
```go
DefaultBashTimeoutMs    = 120_000
DefaultMaxBashTimeoutMs = 600_000
```
To:
```go
DefaultShellTimeoutMs    = 120_000
DefaultMaxShellTimeoutMs = 600_000
```

**Step 2: Update config.go Settings struct**

Change the struct fields (lines 41-42):
```go
ShellTimeoutMs    int `json:"shellTimeoutMs,omitempty"`
MaxShellTimeoutMs int `json:"maxShellTimeoutMs,omitempty"`
```

Update all references in `Load()` (lines 79-80, 101-106):
- `BashTimeoutMs` -> `ShellTimeoutMs`
- `MaxBashTimeoutMs` -> `MaxShellTimeoutMs`

Update `LoadAnnotated()` (lines 187-220):
- Change the sources map keys from `"bashTimeoutMs"` / `"maxBashTimeoutMs"` to `"shellTimeoutMs"` / `"maxShellTimeoutMs"`
- Change all field references from `BashTimeoutMs` / `MaxBashTimeoutMs` to `ShellTimeoutMs` / `MaxShellTimeoutMs`

**Step 3: Update skeleton.go**

In `SkeletonJSON()` (lines 29-30), change:
```
"bashTimeoutMs": 0,
"maxBashTimeoutMs": 0,
```
To:
```
"shellTimeoutMs": 0,
"maxShellTimeoutMs": 0,
```

**Step 4: Update config_test.go**

Change all `BashTimeoutMs` references to `ShellTimeoutMs` (lines 231-232, 241-242).

**Step 5: Update skeleton_test.go**

Change `"bashTimeoutMs", "maxBashTimeoutMs"` to `"shellTimeoutMs", "maxShellTimeoutMs"` (line 27).

**Step 6: Also update shell.go to use new constant names**

In `gohome/internal/tools/shell.go`, change:
- `config.DefaultBashTimeoutMs` -> `config.DefaultShellTimeoutMs`
- `config.DefaultMaxBashTimeoutMs` -> `config.DefaultMaxShellTimeoutMs`

**Step 7: Run tests**

Run: `go test ./gohome/internal/config/ -v`
Expected: All tests PASS

**Step 8: Commit**

```bash
git add gohome/internal/config/ gohome/internal/tools/shell.go
git commit -m "refactor: rename BashTimeoutMs to ShellTimeoutMs in config"
```

---

### Task 3: Rename guard/whitelist internals

**Files:**
- Modify: `gohome/internal/guard/whitelist.go:8`
- Modify: `gohome/internal/guard/compile.go:14,38-53,60-76`
- Modify: `gohome/internal/guard/check.go:86-95`
- Modify: `gohome/internal/guard/suggest.go:8-9,11,13,27`
- Modify: `gohome/internal/guard/persist.go:11-14,28-46,94-96`

**Step 1: Update whitelist.go**

Change the JSON struct field (line 8):
```go
Shell []string `json:"shell"`
```

**Step 2: Update compile.go**

- Rename field `bash` to `shell` in Whitelist struct (line 14)
- Update comment on line 21: "Every shell pattern is auto-anchored..."
- Change `global.Bash` / `project.Bash` to `global.Shell` / `project.Shell` (lines 39-41)
- Change log message from `"guard: skipping invalid bash pattern"` to `"guard: skipping invalid shell pattern"` (line 50)
- Change `wl.bash` to `wl.shell` (line 53)
- Update Allows comment: `For tool == "shell"` (lines 60-61)
- Change `tool == "bash"` to `tool == "shell"` (line 64)
- Change `w.bash` to `w.shell` (line 72)

**Step 3: Update check.go**

- Change comment on line 86: `"For shell it returns..."` 
- Change `tool == "bash"` to `tool == "shell"` (line 88)

**Step 4: Update suggest.go**

- Update all comments referencing "bash" to "shell" (lines 8-9, 11, 13)
- Change `tool != "bash"` to `tool != "shell"` (line 27)
- Update `buildPrefix` comment (line 47): "shell command" instead of "bash command"

**Step 5: Update persist.go**

- Update comments: "shell pattern" instead of "bash pattern" (lines 11, 14)
- Change `tool == "bash"` to `tool == "shell"` (lines 28, 94)
- Change `w.bash` to `w.shell` (lines 35, 44)
- Change `wf.Bash` to `wf.Shell` (lines 95-96)

**Step 6: Update all guard test files**

All `"bash"` string literals in test files change to `"shell"`:
- `compile_test.go`: lines 59, 79, 82, 101 -- change `"bash"` to `"shell"` in `Allows()` calls
- `persist_test.go`: lines 20, 26, 86-87, 119, 148, 188, 193 -- change `"bash"` to `"shell"` in `AddProject()` and `Allows()` calls
- `guard_test.go`: lines 57, 128, 142, 179, 200, 221, 243 -- change `"bash"` to `"shell"` in `Check()` calls
- `suggest_test.go`: lines 24, 33, 42, 51, 60, 69, 78, 84, 92 -- change `"bash"` to `"shell"` in `Suggest()` calls

Also update any `WhitelistFile` literals in tests: change `.Bash` field to `.Shell`.

**Step 7: Run tests**

Run: `go test ./gohome/internal/guard/ -v`
Expected: All tests PASS

**Step 8: Commit**

```bash
git add gohome/internal/guard/
git commit -m "refactor: rename bash to shell in guard/whitelist internals"
```

---

### Task 4: Update TUI references

**Files:**
- Modify: `gohome/internal/tui/chat.go:722,769`
- Modify: `gohome/internal/tui/config_overlay.go:89-90`
- Modify: `gohome/internal/tui/approval_test.go` (all `"bash"` test literals)
- Modify: `gohome/internal/tui/config_overlay_test.go` (lines 15, 28-29, 51)

**Step 1: Update chat.go**

Change `case "bash":` to `case "shell":` on lines 722 and 769.

**Step 2: Update config_overlay.go**

Change (lines 89-90):
```go
writeField("shellTimeoutMs", co.ann.ShellTimeoutMs)
writeField("maxShellTimeoutMs", co.ann.MaxShellTimeoutMs)
```

**Step 3: Update config_overlay_test.go**

- Change `BashTimeoutMs: 60000` to `ShellTimeoutMs: 60000` (line 15)
- Change `"bashTimeoutMs": config.SourceGlobal` to `"shellTimeoutMs": config.SourceGlobal` (line 28)
- Change `"maxBashTimeoutMs": config.SourceDefault` to `"maxShellTimeoutMs": config.SourceDefault` (line 29)
- Change the assertion string from `"bashTimeoutMs"` to `"shellTimeoutMs"` (line 51)

**Step 4: Update approval_test.go**

Change all `"bash"` string literals to `"shell"` in `makeApprovalReq` calls (approximately 15 occurrences across lines 39, 66, 90, 116, 144, 179, 213, 243, 276, 289, 308, 333, 365, 390, 424, 484).

**Step 5: Update golden snapshot files**

Run: `go test ./gohome/internal/tui/ -run TestSnapshots -update`

This regenerates all golden files. Verify the diffs show "bash" -> "shell" in:
- `testdata/TestSnapshots/with_approval_prompt.golden`
- `testdata/TestSnapshots/with_cross_session_approval.golden`
- `testdata/TestSnapshots/config_overlay_empty.golden`
- `testdata/TestSnapshots/config_overlay_populated.golden`

**Step 6: Run tests**

Run: `go test ./gohome/internal/tui/ -v`
Expected: All tests PASS

**Step 7: Commit**

```bash
git add gohome/internal/tui/
git commit -m "refactor: rename bash to shell in TUI display and tests"
```

---

### Task 5: Update main.go registration and system prompt

**Files:**
- Modify: `gohome/cmd/gohome/main.go:274-276,375-376`

**Step 1: Update tool registration**

Change (lines 274-277):
```go
registry.Register(tools.ShellTool{
	DefaultTimeoutMs: settings.ShellTimeoutMs,
	MaxTimeoutMs:     settings.MaxShellTimeoutMs,
})
```

**Step 2: Update system prompt**

Change (line 376):
```
You have access to tools for reading and writing files, running shell commands, and spawning subagents for parallel work.
```

**Step 3: Run full test suite**

Run: `go test ./gohome/...`
Expected: All tests PASS

**Step 4: Run vet and lint**

Run:
```bash
go vet ./gohome/...
golangci-lint run ./gohome/...
```
Expected: No errors

**Step 5: Build**

Run: `go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome`
Expected: Build succeeds

**Step 6: Commit**

```bash
git add gohome/cmd/gohome/main.go
git commit -m "refactor: register ShellTool, update system prompt"
```

---

### Task 6: Update CLAUDE.md and README references

**Files:**
- Modify: `CLAUDE.md` (any references to "bash" tool name)
- Modify: `README.md` (if it references bash whitelist patterns or tool name)

**Step 1: Search for references**

Run:
```bash
grep -n "bash" CLAUDE.md README.md
```

Update any references to the "bash" tool name or whitelist key to "shell". Leave references to actual bash shell commands (like `go test`, `go build`) unchanged -- those are shell commands the user runs, not the tool name.

**Step 2: Commit**

```bash
git add CLAUDE.md README.md
git commit -m "docs: update bash -> shell references in CLAUDE.md and README"
```

---

### Task 7: Final verification

**Step 1: Run the full test suite across all packages**

Run: `go test ./gohome/... -count=1`
Expected: All tests PASS

**Step 2: Run vet and lint**

Run:
```bash
go vet ./gohome/...
golangci-lint run ./gohome/...
```
Expected: Clean

**Step 3: Grep for stale "bash" references**

Run:
```bash
grep -rn '"bash"' gohome/ --include="*.go" | grep -v vendor
```
Expected: No results (all renamed to "shell")

**Step 4: Build and verify**

Run: `go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome`
Expected: Build succeeds, binary runs
