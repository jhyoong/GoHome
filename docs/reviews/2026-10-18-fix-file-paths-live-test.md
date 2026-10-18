# Live test: fix/file-paths (2026-10-18)

Branch `fix/file-paths` fixes FOLLOWUP_TASKS items 39, 52 and 53.

## Changes that reach the binary

- `tools/write.go`: a successful `write` marks the file as read (52).
- `session/session.go`: `normalizeReadPath` resolves symlinks. If the file does not exist, it resolves the parent folder and joins the base name (53).
- `session/paths.go`: `ProjectSlug` resolves symlinks in the cwd. The displayed cwd is unchanged (39).

No dependency, toolchain or workflow changes.

## Results

### Build and static checks

| Check | Result |
|---|---|
| `go vet` | Pass |
| `go test -count=1 ./gohome/...` | Pass |
| golangci-lint v2.15.0 | Pass (0 issues) |
| Cross-build, 25 MB guard | Pass (largest: windows/amd64 17395 KB) |
| govulncheck (branch and main) | No vulnerabilities |

### E2E (`local` endpoint 192.168.50.210, openai wire)

| Test | Result |
|---|---|
| All 11 tests | Pass |

### Headless

| Test | local-openai | local-direct-anthropic |
|---|---|---|
| T1 tool chain | Pass | Pass |
| T2 subagent | Pass | Pass |
| T3 JSONL multi-turn | Pass | Pass |

T1 now runs `write, edit, shell, read`: the model edits right after writing, with no `read` in between, and neither log contains `must be read first`.

### Compaction

| Model | Summaries | Result |
|---|---|---|
| local-openai | 9 | Pass |
| local-direct-anthropic | 4 | Pass |

Every summary describes the earlier turns. No tool errors.

### Item-specific checks (branch vs main)

| Check | Branch | main |
|---|---|---|
| Headless: `read /tmp/<dir>/a.txt`, then `edit /private/tmp/<dir>/a.txt` | Pass: edit applied, no errors | Fail: 2x `must be read first`, file unchanged |
| TUI: session in `/tmp/<dir>`, `/quit`, `--resume` from `/private/tmp/<dir>` | Pass: same JSONL, model recalls the codeword | Fail: new session folder, no prior context |

On the branch, the status bar still shows the path as given (`/private/tmp/...`).

## Not tested

- The general TUI checklist (markdown, approval menu, model switch). This branch does not change TUI code.
- Windows. Symlink unit tests skip if `os.Symlink` fails.
- Resuming a session saved by an older version from a symlinked path. Per the CHANGELOG note, such sessions get a new slug and no longer show in `/resume`.

## New issues

None.
