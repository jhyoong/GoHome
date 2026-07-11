# Token Tracking Fix, CWD in Statusbar, Send Feedback Spinner

Date: 2026-07-11

## Problem

Three TUI issues need fixing:

1. Token tracking only shows the most recent turn's usage, not the cumulative session total.
2. The statusbar does not show the current working directory.
3. No visual feedback between pressing Enter and the first streaming event arriving from the server.

## Design

### 1. Fix cumulative token tracking

**Root cause:** `model_agent.go` line 124 does `sv.Usage = *ev.Usage`, replacing the struct each turn. The Anthropic and OpenAI adapters both return per-turn usage, so only the last turn's counts are visible.

**Fix:** Accumulate instead of replace:

```go
case agent.EventUsageUpdated:
    if ev.Usage != nil {
        sv.Usage.InputTokens      += ev.Usage.InputTokens
        sv.Usage.OutputTokens     += ev.Usage.OutputTokens
        sv.Usage.CacheReadTokens  += ev.Usage.CacheReadTokens
        sv.Usage.CacheWriteTokens += ev.Usage.CacheWriteTokens
        m.checkContextWarnings(sv)
    }
```

**Files:** `gohome/internal/tui/model_agent.go`

### 2. Add CWD to the statusbar

**What:** Show the current working directory in the statusbar between the project/branch segment and the model name. Shorten paths under the home directory with `~`.

**Format:** `projectDir (branch) . ~/path/to/cwd . model . [===] 12.3k/200k (6%)`

**Shortening:** If `m.cwd` starts with `m.homeDir`, replace that prefix with `~`.

**Files:** `gohome/internal/tui/statusbar.go`

### 3. Start spinner immediately on message send

**What:** Start the spinner with "Sending..." in the Enter key handler immediately after setting `sv.InFlight = true`. The spinner message transitions naturally to "Thinking..." or "Generating..." when the first streaming event arrives.

```go
m.spinner.Start("Sending...")
m.spinner.SetOnCancel(m.cancelFocusedSession)
cmds = append(cmds, SpinnerTickCmd())
```

**Files:** `gohome/internal/tui/model_keys.go`

## Testing

- Update existing snapshot golden files affected by the statusbar change.
- Add test for cumulative token accumulation (multiple EventUsageUpdated events should sum).
- Verify spinner starts on send in existing test infrastructure.
