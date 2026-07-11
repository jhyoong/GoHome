# gohome

`gohome` is a lightweight, single-binary coding agent and terminal UI written in Go. The entire stack compiles to a single small binary with no external runtime dependencies.

## Why am I building this

For fun, mostly. Since I made a dumb choice of buying an annual subscription, I might as well use it. I mainly wanted a terminal tool for custom, local LLM endpoints — for both Anthropic and OpenAI-compatible formats. Other alternatives out there are either massive projects or not exactly what I wanted: a simple and lightweight TUI based coding agent with tool-call guardrails with human approval (plus a skip/"yolo" mode) and built-in sequential subagents you can focus into, steer, and approve independently. 

---

## Install

### Download a pre-built binary

Pre-built binaries are attached to each [GitHub release](https://github.com/jhyoong/GoHome/releases). Pick the one matching your platform:

| Platform | Binary name |
|---|---|
| Linux x64 | `gohome-linux-amd64` |
| macOS Apple Silicon | `gohome-darwin-arm64` |
| macOS Intel | `gohome-darwin-amd64` |
| Windows x64 | `gohome-windows-amd64.exe` |

#### macOS / Linux

```sh
# Download (replace the URL with the latest release and your platform)
curl -L -o gohome https://github.com/jhyoong/GoHome/releases/download/v0.4.0/gohome-darwin-arm64

# Make it executable
chmod +x gohome

# Move it somewhere on your PATH
sudo mv gohome /usr/local/bin/

# Verify
gohome --version
```

#### Windows

1. Download `gohome-windows-amd64.exe` from the [latest release](https://github.com/jhyoong/GoHome/releases/latest).
2. Rename it to `gohome.exe` (optional, for convenience).
3. Move it to a directory on your `PATH`, or run it directly.

### Build from source

The source tree lives under `gohome/` at the repo root. Because the module layout places source in a subdirectory named `gohome/`, the binary **must** be built with an explicit `-o` flag; a bare `go build ./gohome/cmd/gohome` would collide with that directory name.

```sh
git clone https://github.com/jhyoong/GoHome
cd GoHome
go build -ldflags "-X main.version=v0.4.0" -o bin/gohome ./gohome/cmd/gohome
```

### Run

```sh
./bin/gohome --model <name>
```

### CLI flags

| Flag | Description |
|---|---|
| `--model <name>` | Select a configured model config by name |
| `--yolo` | Start with all approval prompts disabled |
| `--resume` | Resume the most recent session for the current working directory |
| `--config` | Print merged configuration and exit |
| `--version` | Print version and exit |

---

## Quickstart

### 1. Create `~/.gohome/settings.json`

```json
{
  "modelConfig": {
    "local-anthropic": {
      "wire": "anthropic",
      "baseURL": "http://localhost:8080",
      "apiKeyEnv": "GOHOME_API_KEY",
      "modelName": "claude-opus-4-7",
      "contextWindow": 200000,
      "maxTokens": 16384,
      "thinkingBudget": 10240,
      "headers": {
        "X-Custom-Header": "value"
      }
    },
    "local-openai": {
      "wire": "openai",
      "baseURL": "http://localhost:8081/v1",
      "apiKeyEnv": "GOHOME_API_KEY",
      "modelName": "gpt-4o",
      "contextWindow": 128000,
      "maxTokens": 16384,
      "reasoningEffort": "high"
    }
  },
  "defaultModel": "local-anthropic",
  "systemPrompt": "You are a helpful coding assistant.",
  "bashTimeoutMs": 120000,
  "maxBashTimeoutMs": 600000,
  "contextWarnPct": 0.80,
  "contextCritPct": 0.95,
  "retryBackoffMs": [250, 1000, 2000]
}
```

Both `"anthropic"` and `"openai"` wires are supported. Set `apiKey` for a literal key or `apiKeyEnv` to read from an environment variable.

**Model config fields:**

| Field | Default | Description |
|---|---|---|
| `wire` | — | Wire protocol: `"anthropic"` or `"openai"` |
| `baseURL` | — | Base URL of the LLM endpoint |
| `apiKey` | — | Literal API key (use `apiKeyEnv` instead to avoid storing secrets) |
| `apiKeyEnv` | — | Environment variable name whose value is used as the API key |
| `modelName` | — | Model name sent to the LLM provider |
| `contextWindow` | — | Context window size in tokens (used for usage display) |
| `maxTokens` | `16384` | Max output tokens per LLM turn |
| `thinkingBudget` | `10240` | Extended thinking token budget (Anthropic wire only; ignored by OpenAI) |
| `reasoningEffort` | — | Reasoning effort level sent as `reasoning_effort` on the OpenAI wire (e.g. `"low"`, `"medium"`, `"high"`). Ignored by Anthropic wire. Free-form string passed through to the API as-is |
| `headers` | — | Custom HTTP headers added to every request to this endpoint. Map of header name to value |

**Top-level fields:**

| Field | Default | Description |
|---|---|---|
| `defaultModel` | — | Name of the model config used when `--model` is not passed |
| `systemPrompt` | — | Custom system prompt sent to the LLM. Overrides the built-in default when set |
| `bashTimeoutMs` | `120000` | Default bash command timeout in milliseconds |
| `maxBashTimeoutMs` | `600000` | Maximum bash command timeout in milliseconds |
| `contextWarnPct` | `0.80` | Context window usage ratio at which a warning is shown (must be < `contextCritPct`) |
| `contextCritPct` | `0.95` | Context window usage ratio at which a critical warning is shown (must be > `contextWarnPct` and <= 1.0) |
| `renderThrottleMs` | `0` | Minimum interval in milliseconds between TUI redraws during token streaming. `0` (default) renders every token; higher values reduce terminal flicker on slow connections |
| `retryBackoffMs` | `[250, 1000, 2000]` | Retry backoff schedule in milliseconds |

### 2. Run

```sh
export GOHOME_API_KEY=<your-key>
./bin/gohome --model local-anthropic
```

Project-level settings live in `./.gohome/settings.json` and are merged on top of global settings at startup.

If `contextWarnPct` >= `contextCritPct`, or either value is outside the (0, 1] range, both are silently reset to their defaults (0.80 and 0.95).

---

## Keybindings

| Key | Action |
|---|---|
| `Enter` | Submit input (or toggle expand/collapse on the selected entry when input is empty) |
| `Up` / `Down` | Move the cursor through timeline entries when input is empty |
| `Shift+Enter` | Insert newline |
| `Ctrl+C` (once) | Cancel current LLM turn or close approval prompt |
| `Ctrl+C` (twice) | Quit |
| `Ctrl+E` | Open current input in external editor (`$VISUAL` / `$EDITOR` / `vi`) |
| `Ctrl+H` | Open help overlay |
| `Ctrl+Right` | Focus next session |
| `Ctrl+Left` | Focus previous session |
| `PgUp` / `PgDn` | Scroll viewport |
| `@` | Trigger file search popup (type a query after `@`) |
| `/` | Open slash command with inline autocomplete |
| `Tab` | Confirm file search selection |
| `1`–`4` | Pick option in approval prompt |
| `e` | Edit suggested bash pattern in approval prompt |
| `Esc` | Deny / close overlay / dismiss file search |

### Timeline cursor

When the input editor is empty, `Up` and `Down` arrow keys move a `>` cursor through the conversation timeline. The cursor highlights the selected entry. Press `Enter` on a tool or thinking entry to expand or collapse its details. Thinking blocks are expanded by default so reasoning content is always visible as it streams in.

---

## Slash commands

| Command | Status |
|---|---|
| `/yolo` | Toggles yolo mode (skip all approval prompts) |
| `/tokens` | Opens token usage detail overlay |
| `/quit` | Exits the process |
| `/cancel` | Cancels the current LLM turn, clears pending message queue |
| `/new` | Starts a new session (requires backend callback via `SetSlashCallbacks`; shows "not configured" until wired) |
| `/resume <id>` | Resumes a previous session by ID (requires backend callback; shows "not configured" until wired) |
| `/model <name>` | Switches the active model config (rebuilds the LLM client). Without arguments, opens a selector listing all configured model configs |
| `/config` | Opens the config overlay showing all settings with source annotations. Press `e` to edit in an external editor |

---

## File layout

```
~/.gohome/
  settings.json          # global model config
  whitelist.json         # global auto-approve rules
  sessions/              # JSONL session transcripts, grouped by project
  logs/                  # structured log files (one per day)
./.gohome/               # project-level overrides (in the working directory)
  settings.json          # project model config overrides
  whitelist.json         # project whitelist; "Allow always" entries land here
```

---

## Guardrails and approval

Before every tool call, `gohome` checks the whitelist. If no rule covers the call, it pauses and shows an approval prompt with four options:

- **Allow once** — permit this call only.
- **Allow always** — write an auto-approve rule to `./.gohome/whitelist.json`.
- **Deny** — block the call and return an error to the agent.
- **Deny + steer** — block the call and inject a message into the agent's context.

`/yolo` skips all prompts without writing whitelist entries.

---

## Subagents

The `subagent` tool spawns a fresh, isolated agent session from within the agent loop. The subagent shares the parent's guard and tool registry (but cannot itself spawn subagents), runs synchronously, and returns its final text as a tool result to the parent. The TUI shows each session in a strip at the top; `Ctrl+Right` / `Ctrl+Left` switches focus.

---

