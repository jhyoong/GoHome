# Live test: /resume age column (HEAD 15a19bc)

Work is on the branch `docs/close-llm-wire-todo` (the planned `fix/resume-age-column` branch was not used). Binary tested: branch build vs a `main` build. Endpoints are local (192.168.x.x). Models: `local-openai`, `local-direct-anthropic`. FOLLOWUP_TASKS items 69 and 38.

## Static checks
| Check | Result |
|---|---|
| `go vet ./gohome/...` | Pass |
| `go test -count=1 ./gohome/...` | Pass |
| `golangci-lint` v2.15.0 (CI version) | Pass (0 issues) |
| Golden snapshots | No change, not updated |

## Build
| Check | Result |
|---|---|
| Branch and base build | Pass |
| Cross-builds (linux, darwin x2, windows), size | Pass (12.3 to 17.5 MB, limit 25 MB) |
| Deps / toolchain diff vs main | None |
| govulncheck | Pass (branch and base clean) |

## Headless (headless.sh)
| Model | T1 tools | T2 subagent | T3 multi-turn |
|---|---|---|---|
| local-openai | Pass | Pass | Pass |
| local-direct-anthropic | Pass | Pass | Pass |

## Item 69: `/resume` list at 50 columns (tmux, local-openai)
A session was started with the first message "Please reply with exactly the word READY and nothing else, no tools needed" (title cut to 40 columns by the browser). The model replied "READY" (normal turn: Pass). After `/quit`, a new gohome was started in the same dir at 50x30 and `/resume` was opened.

Branch:
```
-> j3hxnwn7                               just now
   Please reply with exactly the word RE  just now
```

Main (same dir, same session):
```
-> 2dgvzzsb                               just now
   Please reply with exactly the word READY  just
```

| Check | Result |
|---|---|
| Age shows in full ("just now") with a long title | Pass on branch. Main shows "just". |
| No line over 50 columns | Pass (widest line 50) |
| Delete confirm (Ctrl+D) still shows, Escape cancels | Pass |
| `/quit` ends tmux session and process | Pass (branch and main) |

The first row in each capture is the new, still blank session of that run. Blank sessions are cleaned up as before.

## Item 38: failing run for `TestPickResume_SkipsChildAndLoadsChildren`
The `l.Depth == 0` filter in `session.List` (`gohome/internal/session/list.go`) was removed for a moment. The test failed because `pickResume` picked the child session:

```
=== RUN   TestPickResume_SkipsChildAndLoadsChildren
    main_test.go:383: pickResume picked &{ID:p1-sub-1 Depth:1 ParentID:p1 Model: ModelConfig: History:[{Role:user Content:[{Kind:text Text:child Signature: Data: ToolUseID: ToolName: InputJSON: ResultText: IsError:false InvalidInput:false}] StopReason:}] StartedAt:2026-10-18 14:53:57.981063 +0000 UTC ParentToolUseID: cwd:/home/user/proj mu:{w:{_:{} mu:{state:0 sema:0}} writerSem:0 readerSem:0 readerCount:{_:{} v:0} readerWait:{_:{} v:0}} readFiles:map[] subagents:0}, want p1
--- FAIL: TestPickResume_SkipsChildAndLoadsChildren (0.06s)
FAIL
FAIL	github.com/jhyoong/GoHome/gohome/cmd/gohome	0.655s
FAIL
```

The file was restored with `git checkout` (`git diff` shows no change to `list.go`). The test then passed:

```
=== RUN   TestPickResume_SkipsChildAndLoadsChildren
--- PASS: TestPickResume_SkipsChildAndLoadsChildren (0.08s)
PASS
ok  	github.com/jhyoong/GoHome/gohome/cmd/gohome	0.852s
```

## Bugs
- 70 (new, minor, also on main): the delete confirm line is not cut to the screen width. It is the prefix, the label and "  delete? y to confirm", so with a 40-column title it is 65 columns on main and 62 on the branch. Found by reading the code; not seen live, since the row tried live had a short label.

## Not tested
- E2E suite and compaction. This change only touches the `/resume` list rendering, so they were skipped.
- The full TUI checklist at 120 columns (markdown, approval, model switch, `--resume`). Only a normal turn, `/resume`, delete confirm and `/quit` were run, on local-openai only.
- The delete confirm line with a long title selected (see 70).
- Against the real Anthropic API (not allowed: local endpoints only).

## Scratch dirs
`/tmp/gh-bin.3wt6` (binaries; the base worktree was removed), `/tmp/gh-tui.TIqO` (TUI, with `resume-branch.txt` and `resume-main.txt` captures), `/tmp/gh-work.Geod`, `/tmp/gh-work.MpOI` (headless). Sessions are in `~/.gohome/sessions/<scratch-dir-name>-*`.
