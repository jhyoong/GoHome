# Consolidate /endpoint and /model into a single /model command

**Date:** 2026-06-14

## Problem

The `/endpoint` and `/model` slash commands overlap. Both present the same `ModelSelectorComponent` UI. Each endpoint config already bundles a model name, so switching endpoints implicitly switches models. Keeping both commands adds complexity without proportional value.

## Decision

Consolidate into a single `/model` slash command and rename `endpoints` to `modelConfig` throughout the codebase.

## Settings schema

Before:

```json
{
  "endpoints": {
    "local-anthropic": {
      "defaultModel": "claude-opus-4-7",
      ...
    }
  },
  "defaultEndpoint": "local-anthropic"
}
```

After:

```json
{
  "modelConfig": {
    "local-anthropic": {
      "modelName": "claude-opus-4-7",
      ...
    }
  },
  "defaultModel": "local-anthropic"
}
```

## Renames

| Before | After |
|--------|-------|
| `config.Endpoint` struct | `config.ModelConfig` |
| `Endpoint.DefaultModel` field | `ModelConfig.ModelName` |
| `Settings.Endpoints` field | `Settings.ModelConfig` |
| `Settings.DefaultEndpoint` field | `Settings.DefaultModel` |
| `SlashCallbacks.SetEndpoint` | Removed (logic merged into `SetModel`) |
| `SlashCallbacks.SetModel` signature | `func(name string) (model string, contextWindow int, err error)` |
| `--endpoint` CLI flag | `--model` |
| `--model` CLI flag (old model-only override) | Removed |
| `session.Session.Endpoint` | `session.Session.ModelConfig` |

## Slash command behavior

- `/model` (no args): Opens `ModelSelectorComponent` listing all named configs from `modelConfig` with their `modelName` as description. On selection, performs full client rebuild (resolves API key, creates new `common.Client`, updates `SessionState`).
- `/model <name>`: Directly selects the named config by key and performs full client rebuild.

## Callback changes

`SlashCallbacks.SetModel` absorbs the current `SetEndpoint` logic:

1. Look up the named config in `settings.ModelConfig`.
2. Resolve API key via `config.ResolveAPIKey`.
3. Build new LLM client via `llm.New`.
4. Swap client in `SessionState.SetClient`.
5. Update `SessionState.SetModel` with `modelName`.
6. Update `session.Endpoint` -> `session.ModelConfig` tracking.
7. Return `(modelName, contextWindow, nil)`.

`SlashCallbacks.SetEndpoint` is removed.

## CLI flag

`--model <name>` selects which named config to use at startup (replaces `--endpoint`). The old `--model` flag (model-string-only override) is removed.

## What is dropped

The ability to change just the model string without rebuilding the client (old `/model` behavior). Each model switch now rebuilds the full client. This is acceptable because:

- The client rebuild is fast (no persistent connections to tear down).
- The config already bundles model + endpoint together.
- If multi-model-per-endpoint is needed later, it can be added as `/model <config> <model-override>`.

## Migration

No backwards compatibility shim. Old `settings.json` files using `endpoints` / `defaultEndpoint` will fail with a JSON unmarshal error. The fix is to rename the keys. This is acceptable for a dev tool with few users.

## Components unchanged

- `ModelSelectorComponent` -- name now fits better.
- `SessionState` mutex-guarded client management.
- `llm.New()` client factory.
- `config.ResolveAPIKey()`.
