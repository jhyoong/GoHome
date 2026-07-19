# Denylist Design

## Summary

Add a denylist guardrail to the `guard` package that automatically rejects shell commands matching user-defined dangerous patterns. The denylist is the highest-priority check -- it fires before yolo mode and the whitelist, ensuring blocked commands can never execute regardless of other settings.

## Motivation

When running in yolo mode, all tool calls are auto-approved. The denylist provides a hard safety net: users define patterns for commands that should never run (e.g., `rm -rf /`, `mkfs`), and those are rejected even when every other guardrail is disabled.

## Scope

- Shell commands only. Tool-level denies are out of scope (see Future Considerations).

## File Format

Locations (same pattern as whitelist):
- Global: `~/.gohome/denylist.json`
- Project: `.gohome/denylist.json`

Schema:

```json
{
  "shell": [
    "rm -rf /",
    "mkfs",
    ":(){ :|:& };:",
    "regex:>\\s*/dev/sd[a-z]"
  ]
}
```

- Plain strings: substring match (case-sensitive). Catches the pattern anywhere in the command, including inside pipes and chains.
- `regex:` prefix: full regular expression (not auto-anchored). The user has full control over anchoring.

## Defaults

Built-in default patterns apply only when neither a global nor project denylist file exists. If the user has any denylist file, built-in defaults are fully replaced by user config.

When both global and project files exist, their `shell` arrays are merged (union).

Default patterns:

```go
var DefaultDenyPatterns = []string{
    "rm -rf /",
    "mkfs",
    ":(){ :|:& };:",
    "regex:>\\s*/dev/sd[a-z]",
}
```

## Guard Check Order

```
1. Denylist check  -> if matched, reject immediately
2. Yolo mode       -> if active, allow
3. Whitelist check -> if matched, allow
4. Prompt user     -> ask frontend for approval
```

## Matching Logic

1. Extract the `command` field from the tool input JSON.
2. For each denylist pattern:
   - If prefixed with `regex:`: compile and test against the full command string.
   - Otherwise: check if the pattern appears as a substring anywhere in the command.
3. First match wins -- return denial immediately.

## Denial Response

The agent receives a `Decision` with:

```go
Decision{
    Allow:    false,
    Reason:   "denylisted",
    DenyInfo: "command denied by denylist: matched pattern 'rm -rf /'"
}
```

`DenyInfo` is a new string field on `Decision`. No TUI prompt is shown. The agent uses this message to self-correct.

## Package Structure

New files in `gohome/internal/guard/`:

| File | Purpose |
|------|---------|
| `denylist.go` | `DenylistFile` struct, `Denylist` compiled type, `CompileDenylist()`, `Denylist.Denies()` method |
| `denylist_test.go` | Unit tests: substring, regex, piped commands, empty denylist |
| `load_denylist.go` | `LoadDenylist(globalPath, projectPath)` with defaults logic |
| `load_denylist_test.go` | File loading, default fallback, merge behavior tests |
| `defaults.go` | `DefaultDenyPatterns` variable |

Modified files:

| File | Change |
|------|--------|
| `guard.go` | Add `DenyInfo string` field to `Decision` |
| `check.go` | Add `denylist *Denylist` field to `Guard`, insert denylist check as step 1 |
| `cmd/gohome/main.go` | Load denylist files, pass to `NewGuard()` |
| `tui/config_wizard.go` | Optional denylist scaffold step after model setup |

## Key Types

```go
// DenylistFile is the on-disk JSON representation.
type DenylistFile struct {
    Shell []string `json:"shell"`
}

// Denylist is the compiled, ready-to-match representation.
type Denylist struct {
    substring []string
    regex     []*regexp.Regexp
}
```

## Config Wizard Update

After model configuration completes, the wizard asks: "Would you like to configure a shell command denylist?" If yes, scaffold `~/.gohome/denylist.json` with the default patterns as a starting point for the user to edit.

## Future Considerations

**Tool-level denies (not in scope):**
A future iteration could extend the denylist to block entire tools by name. This is more complex because it intersects with the whitelist, needs careful UX (blocking core tools could make the agent unusable), and requires granularity like path-based conditions.

**Other potential enhancements (not in scope):**
- Per-pattern custom denial messages
- `denylist.local.json` for personal overrides not committed to the repo
- A `/denylist` slash command to add patterns at runtime
