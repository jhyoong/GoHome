# E2E Tests

End-to-end tests that run against a live LLM endpoint. These are never run in CI -- they require a real API and use the `e2e` build tag.

## Setup

Copy the example config and fill in your values:

```sh
cp e2e.config.example.json e2e.config.json
```

```json
{
  "endpoint": "http://192.168.1.100:8080",
  "wire": "openai",
  "model": "your-model-name",
  "api_key": "your-api-key"
}
```

`e2e.config.json` is gitignored. Environment variables (`GOHOME_E2E_ENDPOINT`, `GOHOME_E2E_WIRE`, `GOHOME_E2E_MODEL`, `GOHOME_E2E_API_KEY`) override the config file if set.

## Running

```sh
# All e2e tests
go test -tags e2e ./gohome/test/e2e/ -v -timeout 300s

# Single test
go test -tags e2e ./gohome/test/e2e/ -run TestE2ESmokeRoundtrip -v
```

## Tests

| Test | What it validates |
|---|---|
| `TestE2ESmokeRoundtrip` | Basic connectivity -- sends a prompt, checks for a non-empty reply |
| `TestE2EHeadlessToolCall` | Read tool round-trip -- writes a temp file, asks the LLM to read it, verifies content comes back |
| `TestE2EShellTool` | Shell tool -- executes a real shell command, verifies output flows back |
| `TestE2EWriteReadChain` | Multi-tool chaining -- LLM writes a file then reads it back, verifying sequential tool use |
| `TestE2EEditTool` | Edit tool with read-before-edit constraint -- reads, edits, re-reads a file |
| `TestE2EToolErrorRecovery` | Error recovery -- read fails on nonexistent file, agent self-corrects and reads fallback |
| `TestE2ESubagentSpawn` | Subagent lifecycle -- parent spawns a child agent, child runs and returns result |
| `TestE2ESessionResume` | Session persistence -- run a conversation, save to JSONL, reload via session.Load, continue |
| `TestE2EHeadlessMultiTurn` | Interactive headless -- JSONL input drives multi-turn conversation, verifies output |
| `TestE2EInteractiveWithTools` | Interactive + tools -- multi-turn headless with read and write tools across turns |
| `TestE2EAutoCompact` | Auto-compaction -- seeds long history, triggers compaction, verifies agent still responds |
