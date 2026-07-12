# Windows Shell Support: Rename "bash" to "shell" with PowerShell Backend

## Problem

The bash tool uses `cmd /c` on Windows, which lacks scripting capabilities. The LLM recognizes this and wraps commands in `powershell -Command "..."`, adding fragile indirection and wasting tokens. The tool name "bash" further misleads the LLM into generating Unix-flavored commands.

## Research

Other coding agents face the same problem:

- **Claude Code** shipped a separate PowerShell tool (v2.1.84) alongside bash, auto-detecting `pwsh.exe` or `powershell.exe`.
- **Cursor** hardcodes PowerShell on Windows with no user override.
- **OpenCode** kept the "bash" name on Windows, causing the LLM to generate wrong commands. Users filed issues requesting the tool be renamed.

## Decision

Rename the tool from "bash" to "shell". On Windows, execute via `powershell.exe -NoProfile -NonInteractive -Command`. On Unix, keep `/bin/sh -c`. This is a clean breaking change (no backwards compatibility shims).

## Target: Modern dev machines (Windows 10/11 with PowerShell 5.1+)

## Design

### 1. Tool Execution (bash.go -> shell.go)

- Rename `BashTool` to `ShellTool`.
- `Name()` returns `"shell"`.
- `Description()` is platform-aware:
  - Unix: `"Execute a shell command (via /bin/sh)."`
  - Windows: `"Execute a PowerShell command (via powershell.exe)."`
- Windows exec changes from `cmd /c <command>` to `powershell.exe -NoProfile -NonInteractive -Command <command>`.
- Unix exec stays as `/bin/sh -c <command>`.
- Input schema unchanged: `command`, `timeout_ms`, `cwd`.

### 2. Guard / Whitelist

- Rename JSON field from `"bash"` to `"shell"` in `WhitelistFile`.
- Rename internal compiled field from `bash` to `shell`.
- All `tool == "bash"` checks become `tool == "shell"`.
- New patterns written under the `"shell"` key.

### 3. TUI Display (chat.go)

- `extractToolArg` and `renderToolSummary`: change `case "bash"` to `case "shell"`.
- Keep the `"$ "` visual prefix for command display.

### 4. Registration & System Prompt (main.go)

- Register `tools.ShellTool{...}` instead of `tools.BashTool{...}`.
- System prompt: "running shell commands" instead of "running bash commands".

### 5. Config Fields

- Rename `BashTimeoutMs` -> `ShellTimeoutMs`, `MaxBashTimeoutMs` -> `MaxShellTimeoutMs`.
- Update `skeleton.go` and `defaults.go`.

### 6. Tests

- Rename `bash_test.go` -> `shell_test.go`.
- Add Windows-specific tests for PowerShell execution.
- Update tool name references in existing tests.

## Breaking Changes

- Tool name changes from `"bash"` to `"shell"` (affects session replays, whitelist files, guard rules).
- Config JSON keys change from `bashTimeoutMs` / `maxBashTimeoutMs` to `shellTimeoutMs` / `maxShellTimeoutMs`.
- Acceptable since GoHome is pre-1.0.
