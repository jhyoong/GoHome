# Outdated components and dead code plan - 2026-10-17

Covers audit section 8 and the remaining dead code and stale comments outside `internal/tui`.
Branch: `fix/outdated-components`.

## 1. Dead code and comments
- Remove the unreachable block at `agent/compact.go:91-94`.
- Fix the "~1s" shutdown claim at `cmd/gohome/main.go:781`.
- Already fixed before this branch: `session/load.go`, `spawn.go`, `turn.go`, `list.go`.

## 2. Go toolchain
- `go 1.26.9` in `go.mod`, `gohome-ci.yml` and `release.yml`.
- Update AGENTS.md.
- Run `govulncheck ./gohome/...` with go1.26.9.

## 3. Dependencies
- `golang.org/x/sys` to latest, chroma v2.27.0, goldmark v1.8.6, `x/exp/golden` v0.1.0, `teatest` latest.
- `go mod tidy`, then build, vet, test, lint, govulncheck.

## 4. Release workflow
- Windows builds natively on `windows-latest` without `-s -w`. Unix targets keep `-s -w`.
- 25 MB size guard on every target.
- Release notes are the tag's CHANGELOG section only. Fail if missing.

## 5. CI workflow
- Actions: checkout v7, setup-go v7, upload-artifact v7, download-artifact v8, golangci-lint-action v9.
- Pin golangci-lint to v2.15.0.
- `-X main.version=ci-<sha>` in CI builds.
- Size guard on all built targets.

## 6. Lint config
- Remove the no-op `disable: [funlen, gochecknoinits]` block.

## 7. Docs
- Status lines in audit section 8.
- CHANGELOG Unreleased entry.
- FOLLOWUP_TASKS: Bubble Tea / Lip Gloss / Bubbles v2 migration.
- AGENTS.md strip-flag note.

## Out of scope
- Charm v2 migration (FOLLOWUP_TASKS).

## Verification limits
Workflow changes can only be YAML-checked locally. They need a PR CI run and a test tag.
