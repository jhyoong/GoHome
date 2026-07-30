# Windows Defender False Positive Mitigations -- Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Reduce Windows Defender false positive detections by embedding PE metadata, building natively on Windows, and updating documentation.

**Architecture:** Add a `goversioninfo`-generated `.syso` resource file containing a Windows application manifest and version info to the main package. Move the Windows binary build from cross-compilation on Ubuntu to a native `windows-latest` CI runner. Update docs to reflect mitigated triggers and warn against stripping Windows builds.

**Tech Stack:** Go, goversioninfo, GitHub Actions, Windows PE resources

---

### Task 1: Create the Windows Application Manifest

**Files:**
- Create: `gohome/cmd/gohome/gohome.manifest`

**Step 1: Create the manifest file**

Create `gohome/cmd/gohome/gohome.manifest` with this content:

```xml
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity
    type="win32"
    name="GoHome.gohome"
    version="0.4.2.0"
    processorArchitecture="amd64"/>
  <description>Terminal coding agent</description>

  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="asInvoker" uiAccess="false"/>
      </requestedPrivileges>
    </security>
  </trustInfo>

  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <!-- Windows 10 / 11 -->
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
    </application>
  </compatibility>

  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <activeCodePage xmlns="http://schemas.microsoft.com/SMI/2019/WindowsSettings">UTF-8</activeCodePage>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2</dpiAwareness>
    </windowsSettings>
  </application>
</assembly>
```

**Step 2: Commit**

```bash
git add gohome/cmd/gohome/gohome.manifest
git commit -m "feat(windows): add application manifest for PE metadata"
```

---

### Task 2: Create the goversioninfo Config

**Files:**
- Create: `gohome/cmd/gohome/versioninfo.json`

**Step 1: Create the versioninfo config**

Create `gohome/cmd/gohome/versioninfo.json` with this content:

```json
{
  "FixedFileInfo": {
    "FileVersion": {
      "Major": 0,
      "Minor": 4,
      "Patch": 2,
      "Build": 0
    },
    "ProductVersion": {
      "Major": 0,
      "Minor": 4,
      "Patch": 2,
      "Build": 0
    },
    "FileFlagsMask": "3f",
    "FileFlags": "",
    "FileOS": "040004",
    "FileType": "01",
    "FileSubType": "00"
  },
  "StringFileInfo": {
    "Comments": "",
    "CompanyName": "gohome (open-source)",
    "FileDescription": "Terminal coding agent",
    "FileVersion": "0.4.2.0",
    "InternalName": "gohome",
    "LegalCopyright": "MIT License",
    "OriginalFilename": "gohome.exe",
    "ProductName": "gohome",
    "ProductVersion": "0.4.2.0"
  },
  "VarFileInfo": {
    "Translation": {
      "LangID": "0409",
      "CharsetID": "04B0"
    }
  },
  "ManifestFilename": "gohome.manifest"
}
```

Key fields:
- `FileDescription` = "Terminal coding agent" -- this shows in Task Manager and Windows Explorer
- `ProductName` = "gohome" -- identifies the application
- `ManifestFilename` = "gohome.manifest" -- tells goversioninfo to embed the manifest from Task 1
- `FileVersion` / `ProductVersion` -- set to current version; update these when cutting releases

**Step 2: Commit**

```bash
git add gohome/cmd/gohome/versioninfo.json
git commit -m "feat(windows): add goversioninfo config for PE version info"
```

---

### Task 3: Generate and Commit the .syso Resource File

**Files:**
- Modify: `gohome/cmd/gohome/main.go` (add `//go:generate` directive)
- Create: `gohome/cmd/gohome/resource_windows.syso` (generated)

**Step 1: Install goversioninfo**

```bash
go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
```

**Step 2: Add the go:generate directive to main.go**

Add this line immediately after the `package main` declaration (line 1) in `gohome/cmd/gohome/main.go`:

```go
//go:generate goversioninfo -o resource_windows.syso
```

The file should start with:

```go
package main

//go:generate goversioninfo -o resource_windows.syso

import (
```

**Step 3: Generate the .syso file**

Run from the repo root:

```bash
cd gohome/cmd/gohome && goversioninfo -o resource_windows.syso && cd ../../..
```

Expected: creates `gohome/cmd/gohome/resource_windows.syso` (a few KB binary file).

**Step 4: Verify the .syso does not break non-Windows builds**

```bash
go build -o /dev/null ./gohome/cmd/gohome
```

Expected: builds successfully. The Go toolchain ignores `.syso` files when `GOOS` is not `windows`.

**Step 5: Verify the .syso links into a Windows build**

```bash
GOOS=windows GOARCH=amd64 go build -o /tmp/gohome-test.exe ./gohome/cmd/gohome
```

Expected: builds successfully. The `.syso` resource is linked into the PE.

**Step 6: Commit**

```bash
git add gohome/cmd/gohome/main.go gohome/cmd/gohome/resource_windows.syso
git commit -m "feat(windows): generate and embed PE resource with manifest and version info"
```

---

### Task 4: Move Windows CI Build to Native Runner

**Files:**
- Modify: `.github/workflows/gohome-ci.yml`

**Step 1: Update the CI workflow**

Replace the entire file `.github/workflows/gohome-ci.yml` with:

```yaml
name: gohome-ci

on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    name: test (${{ matrix.os }})
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.25"
      - name: go vet
        run: go vet ./gohome/...
      - name: golangci-lint
        uses: golangci/golangci-lint-action@v7
        with:
          args: ./gohome/...
      - name: go test
        run: go test ./gohome/...

  build:
    name: cross-build
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.25"
      - name: build linux/amd64
        env:
          GOOS: linux
          GOARCH: amd64
        run: go build -o dist/gohome-linux-amd64 ./gohome/cmd/gohome
      - name: binary size guard (linux/amd64 <= 25 MB)
        run: |
          size=$(du -k dist/gohome-linux-amd64 | cut -f1)
          echo "size=${size}KB"
          if [ "$size" -gt 25600 ]; then
            echo "binary too large: ${size}KB > 25600KB"
            exit 1
          fi
      - name: build darwin/arm64
        env:
          GOOS: darwin
          GOARCH: arm64
        run: go build -o dist/gohome-darwin-arm64 ./gohome/cmd/gohome
      - name: build darwin/amd64
        env:
          GOOS: darwin
          GOARCH: amd64
        run: go build -o dist/gohome-darwin-amd64 ./gohome/cmd/gohome
      - name: upload binaries
        uses: actions/upload-artifact@v4
        with:
          name: gohome-binaries-unix
          path: dist/
          retention-days: 7

  build-windows:
    name: build windows/amd64 (native)
    runs-on: windows-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.25"
      - name: build windows/amd64
        run: go build -o dist/gohome-windows-amd64.exe ./gohome/cmd/gohome
      - name: upload binary
        uses: actions/upload-artifact@v4
        with:
          name: gohome-binaries-windows
          path: dist/
          retention-days: 7
```

Key changes from the original:
- Removed the `build windows/amd64` step from the `build` job (the one that cross-compiled with `GOOS=windows` on Ubuntu)
- Added a new `build-windows` job that runs on `windows-latest` and builds natively
- Split upload artifacts into `gohome-binaries-unix` and `gohome-binaries-windows` (GitHub Actions requires unique artifact names per job)
- Binary size guard stays on linux/amd64 only

**Step 2: Verify the workflow is valid YAML**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/gohome-ci.yml'))"
```

Expected: no output (valid YAML).

**Step 3: Commit**

```bash
git add .github/workflows/gohome-ci.yml
git commit -m "ci: build Windows binary natively on windows-latest runner"
```

---

### Task 5: Update docs/windows-defender.md

**Files:**
- Modify: `docs/windows-defender.md`

**Step 1: Update the document**

Replace the contents of `docs/windows-defender.md` with:

```markdown
# Windows Defender False Positive

Windows Defender (and some other antivirus engines) may flag the gohome binary as a trojan or potentially unwanted program. This is a false positive.

## Why it happens

gohome is a terminal coding agent. Its core features — executing shell commands, writing files, and communicating with remote LLM APIs — overlap with behavioral patterns that antivirus heuristics associate with malware. The combination of several of these patterns in a single binary pushes the heuristic score above detection thresholds.

## Specific triggers

| Behavior | Why AV flags it | Why it is a false positive | Status |
|---|---|---|---|
| Invokes `powershell.exe -NoProfile -NonInteractive` with arbitrary commands | Matches RAT/backdoor command execution patterns | Core feature: executes commands requested by the user or LLM | Active |
| Writes arbitrary content to arbitrary file paths | Matches dropper/payload-writing behavior | Core feature: the file write and edit tools for code editing | Active (inherent to function) |
| Makes outbound HTTPS requests to configurable remote endpoints | Matches command-and-control (C2) communication | Connects to LLM API endpoints (Anthropic, OpenAI-compatible) | Active (inherent to function) |
| Binary is stripped of debug symbols (`-s -w` linker flags) | Common in malware to hinder reverse engineering | Standard Go release build optimization to reduce binary size | **Mitigated** -- CI and release builds no longer strip Windows binaries |
| Binary is unsigned (no Authenticode certificate) | Unknown publisher, higher threat score from SmartScreen | Open-source project, code signing not yet set up | Active |
| Reads API keys from environment variables and sends them as HTTP headers | Matches credential harvesting behavior | Standard API authentication for LLM endpoints | Active (inherent to function) |
| Can run headlessly with no user interaction (`--yolo --prompt`) | Matches automated payload delivery | Designed for scripted/CI usage of the coding agent | Active (inherent to function) |
| Spawns sub-processes (subagents) that independently execute commands | Matches multi-stage dropper behavior | Subagent feature for parallel coding tasks | Active (inherent to function) |
| Accesses the clipboard | Common data exfiltration vector | Copy/paste support in the TUI | Active (inherent to function) |
| No PE version info or application manifest | Missing metadata increases suspicion score | Open-source project without Windows-specific build tooling | **Mitigated** -- binary now embeds manifest and version info |
| Cross-compiled from Linux | PE internal structures differ from native builds, triggering entropy scanners | Standard Go cross-compilation | **Mitigated** -- Windows binary is now built natively on Windows |

## Workaround

Add a Windows Defender exclusion for the gohome binary:

### Via Windows Security settings

1. Open **Windows Security** (search "Windows Security" in the Start menu).
2. Go to **Virus & threat protection** > **Virus & threat protection settings** > **Manage settings**.
3. Scroll to **Exclusions** > **Add or remove exclusions**.
4. Click **Add an exclusion** > **File**, then select `gohome.exe`.

### Via PowerShell (administrator)

```powershell
Add-MpPreference -ExclusionPath "C:\path\to\gohome.exe"
```

Replace the path with the actual location of your gohome binary.

## Future mitigations

These are planned improvements that may reduce or eliminate remaining false positives:

- **Authenticode code signing** with an EV certificate to establish publisher trust.
- **Submitting the binary to Microsoft** via their false positive reporting portal for whitelisting.
- **Switching the default Windows shell from PowerShell to cmd.exe** to reduce the RAT/backdoor heuristic match. Users who need PowerShell can invoke it explicitly. This is a next step if AV flagging persists after the current mitigations.
```

**Step 2: Commit**

```bash
git add docs/windows-defender.md
git commit -m "docs: update windows-defender.md with mitigated triggers and status column"
```

---

### Task 6: Update CLAUDE.md and README.md Build Examples

**Files:**
- Modify: `CLAUDE.md`
- Modify: `README.md`

**Step 1: Update CLAUDE.md**

In `CLAUDE.md`, find the build section (line 13):

```sh
go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome
```

Add a note below the build command, inside the same code block section. After the closing triple-backtick of the existing build block, add:

```markdown
**Windows note:** Do not use `-ldflags "-s -w"` (strip flags) when building for Windows. Stripped binaries score higher on antivirus heuristics. The CI workflow builds without strip flags. See `docs/windows-defender.md` for details.
```

**Step 2: Update README.md**

In `README.md`, find the "Build from source" section (line 50-54). After the existing build command code block, add the same Windows note:

```markdown
> **Windows note:** Do not use `-ldflags "-s -w"` (strip flags) when building for Windows. Stripped binaries score higher on antivirus heuristics. See [Windows Defender False Positive](docs/windows-defender.md) for details.
```

**Step 3: Commit**

```bash
git add CLAUDE.md README.md
git commit -m "docs: add Windows strip flag warning to build instructions"
```

---

### Task 7: Verify Full Build on All Platforms

**Step 1: Verify macOS/Linux build still works**

```bash
go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome
```

Expected: builds successfully, no errors.

**Step 2: Verify Windows cross-build includes the .syso**

```bash
GOOS=windows GOARCH=amd64 go build -ldflags "-X main.version=dev" -o /tmp/gohome-test.exe ./gohome/cmd/gohome
file /tmp/gohome-test.exe
```

Expected: `PE32+ executable (console) x86-64, for MS Windows`

**Step 3: Run all tests**

```bash
go test ./gohome/...
```

Expected: all tests pass.

**Step 4: Run vet and lint**

```bash
go vet ./gohome/...
golangci-lint run ./gohome/...
```

Expected: no errors.

**Step 5: Commit (if any fixes were needed)**

Only commit if previous steps revealed issues that needed fixing.
