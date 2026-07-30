# Windows Defender False Positive Mitigations -- Design

**Date:** 2026-07-30
**Status:** Approved

## Problem

Windows Defender (and other AV engines) flag the gohome binary as a trojan or PUP. The binary's behavioral patterns -- shell execution, arbitrary file writes, outbound HTTPS, missing PE metadata -- overlap with malware heuristics. See `docs/windows-defender.md` for the full trigger table.

## Scope

Codebase and CI changes only. External processes (Authenticode code signing, Microsoft false positive submission) are out of scope for this design.

## Changes

### 1. Windows Application Manifest and Version Info

Embed PE metadata into the Windows binary so it looks like a legitimate application.

- Add `goversioninfo` as a `go generate` tool. It reads `gohome/cmd/gohome/versioninfo.json` and produces `resource_windows.syso`.
- Create `gohome/cmd/gohome/versioninfo.json` with ProductName, FileDescription, CompanyName, LegalCopyright, and version fields.
- Create `gohome/cmd/gohome/gohome.manifest` declaring the binary as a standard Windows application: `asInvoker` execution level, Windows 10/11 compatibility, UTF-8 and DPI awareness.
- Add a `//go:generate` directive in `main.go`.
- Commit the generated `.syso` file so CI does not need `goversioninfo` installed.
- The `.syso` file is only linked when `GOOS=windows`; other platforms are unaffected.

**Mitigates:** "Unknown publisher" heuristic, missing PE metadata, SmartScreen score.

### 2. Remove Strip Flags for Windows Builds

Preserve debug symbols in the Windows binary.

- CI already builds without `-s -w`, so no workflow change is needed.
- Update CLAUDE.md and README.md build examples to note that `-s -w` should not be used for Windows.
- Any future release automation must not strip the Windows binary.

**Mitigates:** "Stripped binary" heuristic used by entropy-based scanners.

### 3. Native Windows CI Build

Build the Windows binary on a Windows runner instead of cross-compiling from Ubuntu.

- Replace the cross-compiled Windows build step in `.github/workflows/gohome-ci.yml` with a separate job (or matrix entry) on `windows-latest`.
- The new job builds with `go build -o dist/gohome-windows-amd64.exe ./gohome/cmd/gohome` (no GOOS/GOARCH needed).
- Upload the artifact alongside other platform binaries.
- Remove the Windows cross-build step from the `ubuntu-latest` job.
- Binary size guard stays on the Linux build.

**Mitigates:** Cross-compilation produces PE internals with different structures that trigger entropy-based scanners.

### 4. Doc Updates

- Update `docs/windows-defender.md` table with a "Status" column marking mitigated triggers.
- Move "switch Windows shell from PowerShell to cmd.exe" into the Future Mitigations section as a next step if AV flagging persists after these changes.
- Keep Authenticode signing and Microsoft submission in Future Mitigations unchanged.
- Update CLAUDE.md/README.md build examples to warn against `-s -w` on Windows.

## What Is NOT Changing

- **Shell tool stays as PowerShell.** The `powershell.exe -NoProfile -NonInteractive` invocation is unchanged. If AV flagging persists after these changes, switching to `cmd.exe /C` is a documented next step.
- **No Authenticode signing.** Requires an EV certificate or Azure Trusted Signing account.
- **No Microsoft submission.** Requires a signed binary and manual portal submission.
- **No `defender-exclude` subcommand.** The workaround is already documented.

## Files Changed

| File | Change |
|---|---|
| `gohome/cmd/gohome/versioninfo.json` | New: goversioninfo config |
| `gohome/cmd/gohome/gohome.manifest` | New: Windows app manifest |
| `gohome/cmd/gohome/resource_windows.syso` | New: generated PE resource |
| `gohome/cmd/gohome/main.go` | Add `//go:generate` directive |
| `.github/workflows/gohome-ci.yml` | Move Windows build to native runner |
| `docs/windows-defender.md` | Add status column, update future mitigations |
| `CLAUDE.md` | Note about `-s -w` on Windows |
| `README.md` | Note about `-s -w` on Windows |
