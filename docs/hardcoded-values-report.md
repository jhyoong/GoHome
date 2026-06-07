# Hardcoded Values Report — GoHome Codebase

Date: 2026-06-07

## 1. API and LLM Configuration

| File | Line | Value | What it is |
|---|---|---|---|
| `cmd/gohome/main.go` | 250 | `128000` | Default context window size |
| `cmd/gohome/main.go` | 271 | `16384` | Max output tokens per turn |
| `cmd/gohome/main.go` | 272 | `10240` | Extended thinking budget |
| `internal/agent/turn.go` | 21 | `4096` | Fallback per-turn token limit |
| `internal/agent/spawn.go` | 24 | `1` | Max subagent nesting depth |
| `internal/tui/model.go` | 157, 199 | `128000` | Context window (duplicated) |
| `internal/tui/model.go` | 478 | `0.95` | Context "near limit" warning threshold |
| `internal/tui/model.go` | 481 | `0.80` | Context "80% full" warning threshold |

## 2. Timeouts, Retries, and Timing

| File | Line | Value | What it is |
|---|---|---|---|
| `internal/tools/bash.go` | 18 | `120_000` ms | Default bash command timeout |
| `internal/tools/bash.go` | 19 | `600_000` ms | Max bash command timeout |
| `internal/tui/model.go` | 559 | `500` ms | Double-tap Ctrl+C quit window |
| `internal/tui/spinner.go` | 12 | `80` ms | Spinner frame interval |
| `internal/tui/filesearch.go` | 131 | `3` s | File search timeout |
| `internal/llm/common/retry.go` | 12 | `[250ms, 1s, 2s]` | Retry backoff schedule |

## 3. Buffer Sizes and Limits

| File | Line | Value | What it is |
|---|---|---|---|
| `internal/tools/bash.go` | 96 | `64KB / 1MB` | Scanner buffer / max |
| `internal/tools/read.go` | 64 | `64KB / 1MB` | Scanner buffer / max |
| `internal/tools/read.go` | 12 | `2000` | Default max lines per read |
| `internal/session/writer.go` | 30 | `64` | Writer channel buffer size |
| `internal/tui/model.go` | 684 | `10` | Message queue capacity |
| `internal/tui/editor.go` | 40 | `100` | Command history length |

## 4. Directory Paths and File Names

| File | Line | Value | What it is |
|---|---|---|---|
| `cmd/gohome/main.go` | 111 | `".gohome"` | Home config directory |
| `cmd/gohome/main.go` | 53 | `"logs"` | Log subdirectory |
| `cmd/gohome/main.go` | 175-176 | `"whitelist.json"` | Global and project whitelist files |
| `internal/config/config.go` | 106 | `"~/.gohome/settings.json"` | Global settings path |
| `internal/config/config.go` | 112 | `".gohome/settings.json"` | Project settings path |
| `internal/session/paths.go` | 23 | `"sessions/<slug>/<date>-<id>.jsonl"` | Session storage pattern |
| `internal/tui/model.go` | 493 | `"gohome-*.md"` | Temp editor file pattern |
| `internal/tui/model.go` | 513 | `"vi"` | Fallback external editor |

## 5. File Permissions

| File | Line | Value | What it is |
|---|---|---|---|
| `cmd/gohome/main.go` | 54 | `0o755` | Directory creation mode |
| `cmd/gohome/main.go` | 61 | `0o644` | File creation mode |
| `internal/tools/write.go` | 49 | `0644` | Written file mode |
| `internal/tools/edit.go` | 82 | `0644` | Edited file mode |
| `internal/guard/persist.go` | 64, 70 | `0o755 / 0o644` | Guard dir/file modes |
| `internal/session/writer.go` | 21, 24 | `0o755 / 0o644` | Session dir/file modes |

## 6. TUI Layout and Dimensions

| File | Line | Value | What it is |
|---|---|---|---|
| `internal/tui/model.go` | 159 | `80 x 24` | Default editor dimensions |
| `internal/tui/model.go` | 163 | `20` | Max chat display lines |
| `internal/tui/model.go` | 53, 56 | `1, 1` | Status bar / session strip height |
| `internal/tui/editor.go` | 12-13 | `3 / 0.3` | Editor min height / max ratio |
| `internal/tui/selectlist.go` | 47 | `10` | Max visible select items |
| `internal/tui/selectlist.go` | 80 | `40` | Width threshold for descriptions |
| `internal/tui/statusbar.go` | 47 | `10` | Progress bar character width |
| `internal/tui/statusbar.go` | 16-22 | `1000` | Token count "k" formatting threshold |
| `internal/tui/session_browser.go` | 21 | `40` | Label truncation width |
| `internal/tui/approval.go` | 124-125 | `4 / 20` | Approval box padding / min width |
| `internal/tui/pending.go` | 28-29 | `10` | Min pending display width |

## 7. Colors

All colors are hardcoded ANSI codes in `internal/tui/`.

| File | Code | Used for |
|---|---|---|
| `chat.go` | `"12"` | User message (bold blue) |
| `chat.go` | `"1"` | Errors/notices (red) |
| `chat.go` | `"2"` | Success (green) |
| `chat.go` | `"3"` | Pending (yellow) |
| `approval.go` | `"3"` | Approval border (yellow) |
| `editor.go` | `"8" / "3"` | Editor border (gray / yellow for bash) |
| `spinner.go` | `"6"` | Spinner (cyan) |
| `statusbar.go` | `"1"` | YOLO badge (red) |
| `progress.go` | `"2" / "3" / "1"` | Progress: ok / warn / error |
| `selectlist.go` | `"1"` | Delete action (red) |

## 8. UI Strings

### Status messages (~20 strings in `model.go`)

- `"Thinking..."`, `"Generating..."`
- `"Cancelled."`, `"Cancelled -- press Ctrl+C again to quit"`
- `"Press Ctrl+C again to quit"`
- `"YOLO mode ON"`, `"YOLO mode OFF"`
- `"No sessions found"`
- `"Message queue full (10)"`
- `"New session: " + id`, `"Resumed: " + id`, `"Deleted session: " + id`
- `"Model set to " + name`, `"Current model: %s"`
- `": unknown command"`
- `"Context near limit -- next turn may fail or truncate."`
- `"Context 80% full -- consider /new or /resume into a fresh session."`
- `"! [%s] needs approval -- Ctrl+] to focus"`
- `"Approval dismissed"`

### Approval overlay (~10 strings in `approval.go`)

- `"Approve tool call -- %s"`, `"Tool: %s"`, `"Command: %s"`
- `"[1] Allow once"`, `"[2] Allow always   pattern: %s"`, `"[3] Deny"`, `"[4] Deny + steer"`
- `"Steer message (Enter to send, Esc to cancel):"`
- `"(Enter to confirm pattern, Esc to cancel edit)"`, `"(e to edit)"`
- `"Esc: deny | arrows to navigate"`

### Chat display (in `chat.go`)

- `"you:"`, `"Thinking..."`, `"Thinking... (%d lines)"`
- `"args: "`, `"result:"`, `"[notice] %s"`

### Slash commands (in `help.go` and `slash.go`)

- `/help`, `/new`, `/resume`, `/yolo`, `/endpoint`, `/model`, `/cancel`, `/tokens`, `/quit`

### Key bindings (in `help.go` and `keys.go`)

- `Ctrl+C`, `Ctrl+E`, `Ctrl+H`, `Ctrl+]`, `Ctrl+[`
- `PgUp/PgDown`, `Enter`, `Alt+Enter`, `Tab`, `Esc`, `@`

## 9. Build and CI

| File | Line | Value | What it is |
|---|---|---|---|
| `cmd/gohome/main.go` | 29 | `"dev"` | Default version string (overridden by ldflags) |
| `cmd/gohome/main.go` | 41 | `5` bytes | Session ID length (8 base32 chars) |
| `cmd/gohome/main.go` | 58 | `"2006-01-02"` | Log date format |
| `cmd/gohome/main.go` | 67 | `"GOHOME_DEBUG"` | Debug env var name |
| `.github/workflows/gohome-ci.yml` | 46 | `25 MB` | Max binary size guard |

## Priority Assessment

### High — values users/operators are likely to want to change

- Context window, max tokens, thinking budget (LLM tuning)
- Bash timeout defaults (workflow dependent)
- Context warning thresholds (80%/95%)
- Retry backoff schedule

### Medium — reasonable to extract into a config struct

- All TUI dimensions and layout values (editor size, chat lines, select list max)
- Color theme (no theming support currently — all ANSI codes inline)
- Key bindings (not rebindable)
- Subagent depth limit

### Low — acceptable as hardcoded for now

- File permissions (`0644`/`0755` are standard Unix conventions)
- Directory names (`.gohome`, `logs`, `sessions`) are identity-level choices
- Date formats, session ID length, spinner interval
- UI strings (would only matter for i18n)
- Build/CI values (binary size guard, version default)
