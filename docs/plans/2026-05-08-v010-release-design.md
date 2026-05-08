# v0.1.0 Release Design

## Overview

Prepare GoHome for its first public release on GitHub. This covers renaming the project from `agent-chat` to `gohome`, setting up a CI pipeline for ongoing quality checks, and a release pipeline that triggers on git tags to produce multi-platform binaries and a Docker image.

## 1. Project Rename

All references to `agent-chat` are renamed to `gohome`:

| Location | Change |
|---|---|
| Binary output name | `agent-chat` → `gohome` |
| Default config dir | `~/.agent-chat/` → `~/.gohome/` |
| Default DB path | `~/.agent-chat/data.db` → `~/.gohome/data.db` |
| Default config path | `~/.agent-chat/config.yaml` → `~/.gohome/config.yaml` |
| Makefile | Binary target and references |
| README | All references |

The Go module path (`github.com/JiaHui/gohome`) already uses the correct name — no `go.mod` changes needed.

## 2. Version Injection

In `cmd/agent/main.go`, change the hardcoded `"dev"` version to a package-level variable:

```go
var version = "dev"
```

GoReleaser injects the git tag at build time via ldflags:

```
-ldflags "-X main.version={{ .Version }}"
```

Local `make build` continues to show `dev`. Tagged releases show the actual version (e.g., `v0.1.0`).

## 3. Dockerfile

Two-stage build:

- **Build stage:** `golang:1.25-alpine` with CGO disabled (safe — `modernc.org/sqlite` is pure Go)
- **Final stage:** `alpine:3.21` for CA certificates and HTTPS support to LLM endpoints
- Exposes port 3000
- Runs as non-root user
- Entrypoint: `gohome`

## 4. CI Workflow (`.github/workflows/ci.yml`)

Triggers on push to `main` and pull requests targeting `main`.

Steps:
1. Checkout
2. Set up Go (version from `go.mod`)
3. Cache Go modules and build cache
4. `go test ./...`
5. `go vet ./...`
6. Install and run `staticcheck ./...`

Runs on `ubuntu-latest` only — cross-platform issues surface in release builds, not unit tests.

## 5. Release Workflow (`.github/workflows/release.yml`)

Triggers on push of tags matching `v*`.

Steps:
1. Checkout with full history (`fetch-depth: 0`)
2. Set up Go
3. Set up QEMU (multi-arch Docker)
4. Set up Docker Buildx
5. Log in to GHCR via built-in `GITHUB_TOKEN`
6. Run GoReleaser

## 6. GoReleaser Config (`.goreleaser.yaml`)

**Builds:** 6 platform combinations
- `linux/amd64`, `linux/arm64`
- `darwin/amd64`, `darwin/arm64`
- `windows/amd64`, `windows/arm64`

**Archives:**
- `.tar.gz` for Linux and macOS
- `.zip` for Windows
- Named `gohome_{{ .Os }}_{{ .Arch }}`

**Checksums:** Single `SHA256SUMS` file covering all archives

**Changelog:** Auto-generated, grouped by conventional commit prefix (`feat`, `fix`, `chore`, etc.). Release is created as a **draft** so the changelog can be reviewed and edited before publishing.

**Docker:** Multi-arch image (`linux/amd64`, `linux/arm64`) pushed to:
- `ghcr.io/<owner>/gohome:{{ .Tag }}`
- `ghcr.io/<owner>/gohome:latest`

No extra secrets needed — uses the built-in `GITHUB_TOKEN`.

## Release Process

```bash
git tag v0.1.0
git push origin v0.1.0
```

This triggers the release workflow. A draft GitHub release is created with the changelog template. Review and publish from the GitHub UI.
