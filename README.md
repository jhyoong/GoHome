# gohome

`gohome` is a lightweight, single-binary coding agent and terminal UI written in Go. It targets custom LLM endpoints — both Anthropic-wire and OpenAI-compatible formats — and provides two features absent from most agents: tool-call guardrails with human approval (plus a skip/"yolo" mode) and built-in sequential subagents you can focus into, steer, and approve independently. The entire stack compiles to a single small binary with no external runtime dependencies.

---

## Build and install

The source tree lives under `gohome/` at the repo root. Because the module layout places source in a subdirectory named `gohome/`, the binary **must** be built with an explicit `-o` flag; a bare `go build ./gohome/cmd/gohome` would collide with that directory name.

```sh
git clone https://github.com/jhyoong/GoHome
cd GoHome
go build -ldflags "-X main.version=v0.2.0" -o bin/gohome ./gohome/cmd/gohome
```

### Run

```sh
./bin/gohome --endpoint <name>
```

Override the model for a single run:

```sh
./bin/gohome --endpoint local-anthropic --model claude-haiku-4-5
```

---

## Quickstart

### 1. Create `~/.gohome/settings.json`

```json
{
  "endpoints": {
    "local-anthropic": {
      "wire": "anthropic",
      "baseURL": "http://localhost:8080",
      "apiKeyEnv": "GOHOME_API_KEY",
      "defaultModel": "claude-opus-4-7",
      "contextWindow": 200000
    },
    "local-openai": {
      "wire": "openai",
      "baseURL": "http://localhost:8081/v1",
      "apiKeyEnv": "GOHOME_API_KEY",
      "defaultModel": "gpt-4o",
      "contextWindow": 128000
    }
  },
  "defaultEndpoint": "local-anthropic"
}
```

Both `"anthropic"` and `"openai"` wires are supported. Set `apiKey` for a literal key or `apiKeyEnv` to read from an environment variable.

### 2. Run

```sh
export GOHOME_API_KEY=<your-key>
./bin/gohome --endpoint local-anthropic
```

Project-level settings live in `./.gohome/settings.json` and are merged on top of global settings at startup.

---

## Keybindings

| Key | Action |
|---|---|
| `Enter` | Submit input |
| `Shift+Enter` | Insert newline |
| `Ctrl+C` (once) | Cancel current LLM turn or close approval prompt |
| `Ctrl+C` (twice) | Quit |
| `Ctrl+]` | Focus next session |
| `Ctrl+[` | Focus previous session |
| `Ctrl+L` | Clear viewport (history preserved in memory) |
| `PgUp` / `PgDn` | Scroll viewport |
| `/` | Open slash command with inline autocomplete |
| `1`–`4` | Pick option in approval prompt |
| `e` | Edit suggested bash pattern in approval prompt |
| `Esc` | Deny / close overlay |

---

## Slash commands

| Command | Status |
|---|---|
| `/yolo` | Implemented — toggles yolo mode (skip all approval prompts) |
| `/tokens` | Implemented — opens token usage detail overlay |
| `/quit` | Implemented — exits the process |
| `/new` | Not implemented in v0.2 (shows "not implemented") |
| `/resume` | Not implemented in v0.2 (shows "not implemented") |
| `/endpoint <name>` | Not implemented in v0.2 (shows "not implemented") |
| `/model <name>` | Not implemented in v0.2 (shows "not implemented") |
| `/cancel [message]` | Not implemented in v0.2 (shows "not implemented") |

---

## File layout

```
~/.gohome/
  settings.json          # global endpoint config
  whitelist.json         # global auto-approve rules
  sessions/              # JSONL session transcripts, grouped by project
  logs/                  # structured log files (one per day)

./.gohome/               # project-level overrides (in the working directory)
  settings.json          # project endpoint/model overrides
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

The `subagent` tool spawns a fresh, isolated agent session from within the agent loop. The subagent shares the parent's guard and tool registry (but cannot itself spawn subagents), runs synchronously, and returns its final text as a tool result to the parent. The TUI shows each session in a strip at the top; `Ctrl+]` / `Ctrl+[` switches focus.

---

