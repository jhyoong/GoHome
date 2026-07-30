# Design: Testing & Fixes for TODO Items 2, 3, 4, 6

Date: 2026-07-30

## Overview

This design covers four TODO items that share a common theme of testing, verification, and documentation:

1. **TODO #2 — Context auto compaction**: Fix the cache invalidation bug via partial compaction, add E2E test.
2. **TODO #3 — Agentic CLI workflow**: Fix the stale E2E smoke test and add E2E tests for headless tool calls and multi-turn interactive mode.
3. **TODO #4 — Windows Defender trojan alert**: Document what triggers the false positive and how users can work around it.
4. **TODO #6 — Denylist testing**: Fix the DenyInfo propagation bug in dispatchTool and add agent-level denylist tests.

All E2E tests use the existing `e2e` build tag and require a live LLM endpoint via environment variables.

---

## 1. Context Auto Compaction — Cache Bug Fix + E2E Test

### Problem

Current `compact()` in `agent/compact.go` replaces the entire `sess.History` with a single user-role summary message. When this new history is sent to the Anthropic API on the next turn, the prompt cache sees a completely different message sequence and treats it as a full cache miss. The old cached prefix is invalidated.

### Solution: Partial Compaction

Change `compact()` to keep the most recent N messages intact and only summarize the older ones.

**Algorithm:**

1. Split history into "old" and "recent" — keep the last K messages (default ~4, roughly the last 2 turns). Don't split in the middle of a tool-use/tool-result pair: if the split lands on a `RoleTool` message, include the preceding assistant message too.
2. Summarize only the "old" portion — send only old messages to the LLM with the compaction prompt.
3. Replace history with `[summary_message] + recent_messages` — the summary becomes the first message, recent messages follow unchanged. This preserves the cache-hot tail.
4. Session load in `load.go` already handles this correctly: the compaction event resets history to the summary, and subsequent JSONL entries (the kept recent messages) are appended on top.

**Changes:**

- `agent/compact.go`: Modify `compact()` to split history and only summarize the old portion.
- `agent/compact_test.go`: Update unit tests for the new split logic, add test for tool-use/tool-result pair boundary handling.

### E2E Test

Add `TestE2EAutoCompact` to `test/e2e/smoke_test.go`:

1. Create an agent with `CompactCfg.Enabled = true`, `Mode = "percentage"`, `TriggerPct = 0.01` (very low threshold to force compaction).
2. Seed history with several user/assistant message pairs to create bulk.
3. Send a prompt that triggers a tool call, pushing usage past the threshold.
4. Verify: compaction fires (EventCompacted in frontend events), the agent continues working after compaction (a second turn completes), and the final response is coherent.

---

## 2. Agentic CLI Workflow — E2E Tests

### Fix Existing Smoke Test

`TestE2ESmokeRoundtrip` references `Agent{Client: ..., Writer: ...}` which no longer exist on the Agent struct. Update to use `State: NewSessionState(sess, w, client)`.

### New Test: One-Shot with Tool Calls

Add `TestE2EHeadlessToolCall`:

1. Create an agent with yolo guard and tool registry (at minimum the read or shell tool).
2. Create a temp file with known content (e.g., write "hello-gohome" to a file).
3. Seed history with: "Read the file /path/to/test.txt and tell me its exact content."
4. Run the agent.
5. Verify: a tool was called (EventToolResult emitted), the final assistant response contains the file content.

### New Test: Multi-Turn Interactive via Headless Frontend

Add `TestE2EHeadlessMultiTurn`:

1. Create a `headless.NewInteractiveFrontend` with a piped `io.Reader` containing two JSONL messages followed by an exit.
2. Wire it as the Frontend.
3. Run the interactive loop (same pattern as main.go).
4. Verify: TurnDone event emitted, the agent responded with text, exit was clean.

### Shared Helper

Extract a `newE2EAgent` helper that reads env vars, creates the LLM client, guard, registry, session, and writer. All E2E tests share this setup.

---

## 3. Windows Defender — Investigation Documentation

### Document

Create `docs/windows-defender.md` with:

1. **Why Windows Defender flags gohome** — One paragraph explaining that gohome's core functionality overlaps with behavioral patterns antivirus heuristics associate with malware.

2. **Specific triggers** — Table of key patterns:

   | Pattern | Why it triggers | Why it's a false positive |
   |---|---|---|
   | `powershell.exe -NoProfile -NonInteractive` with arbitrary commands | RAT/backdoor signature | Core feature: executing user/LLM-requested commands |
   | File writes to arbitrary paths | Dropper behavior | Core feature: code editing tool |
   | Outbound HTTPS to configurable endpoints | C2 communication | Connecting to LLM API endpoints |
   | Stripped binary (`-s -w`) | Anti-analysis evasion | Standard Go release build optimization |
   | Unsigned binary | Unknown publisher | Open-source project, no Authenticode cert yet |
   | API keys from env vars sent over HTTP | Credential theft | Standard API authentication |

3. **Workaround** — Step-by-step instructions for adding a Windows Defender exclusion.

4. **Future mitigations** — Authenticode signing, Microsoft false positive submission, native Windows builds, removing `-s -w` from Windows builds.

### README Change

Add a "Windows Defender False Positive" entry under a Troubleshooting heading, linking to the doc.

---

## 4. Denylist — Bug Fix + Agent-Level Tests

### Bug

In `agent/run.go` `dispatchTool()`, when the denylist blocks a command:

```go
if !dec.Allow {
    if dec.SteerMessage != "" {
        return dec.SteerMessage, true, 0, false
    }
    return "Tool call denied by user.", true, 0, true
}
```

Two issues:
1. Returns "Tool call denied by user." for denylist rejections — misleading, the denylist blocked it.
2. Sets `denied = true`, halting the agent via `ErrToolDenied`. The LLM never gets a chance to self-correct.

The `dec.DenyInfo` field (containing "command denied by denylist: matched pattern 'X'") is never read.

### Fix

Add a branch checking `dec.DenyInfo` before the generic denial path:

```go
if !dec.Allow {
    if dec.DenyInfo != "" {
        return dec.DenyInfo, true, 0, false  // agent can self-correct
    }
    if dec.SteerMessage != "" {
        return dec.SteerMessage, true, 0, false
    }
    return "Tool call denied by user.", true, 0, true
}
```

- Denylist rejections: return informative message, `denied = false`, agent continues.
- Steer rejections: unchanged.
- User denials: unchanged, agent halts.

### Agent-Level Tests

Add to `agent/run_test.go`:

**`TestRun_DenylistBlocksShellCommand`** — Wire a denylist with "rm -rf" into a yolo guard. LLM requests `shell` with `rm -rf /tmp/foo`. Verify: tool not executed, result contains denylist info, Run does NOT return ErrToolDenied, agent continues to second turn.

**`TestRun_DenylistNonShellPassthrough`** — Same denylist. LLM requests a non-shell tool. Verify: denylist does not block it, tool executes normally.

**`TestRun_DenylistAgentSelfCorrects`** — Turn 1: shell with "rm -rf /tmp" blocked. Turn 2: shell with "rm /tmp/foo.txt" allowed. Turn 3: end_turn. Verify: first blocked, second executed, agent completed.

**Helper:** `compileDenylistGuard(t, patterns)` — creates a guard with a real denylist, yolo mode on.
