# Windows Defender False Positive

Windows Defender (and some other antivirus engines) may flag the gohome binary as a trojan or potentially unwanted program. This is a false positive.

## Why it happens

gohome is a terminal coding agent. Its core features — executing shell commands, writing files, and communicating with remote LLM APIs — overlap with behavioral patterns that antivirus heuristics associate with malware. The combination of several of these patterns in a single binary pushes the heuristic score above detection thresholds.

## Specific triggers

| Behavior | Why AV flags it | Why it is a false positive |
|---|---|---|
| Invokes `powershell.exe -NoProfile -NonInteractive` with arbitrary commands | Matches RAT/backdoor command execution patterns | Core feature: executes commands requested by the user or LLM |
| Writes arbitrary content to arbitrary file paths | Matches dropper/payload-writing behavior | Core feature: the file write and edit tools for code editing |
| Makes outbound HTTPS requests to configurable remote endpoints | Matches command-and-control (C2) communication | Connects to LLM API endpoints (Anthropic, OpenAI-compatible) |
| Binary is stripped of debug symbols (`-s -w` linker flags) | Common in malware to hinder reverse engineering | Standard Go release build optimization to reduce binary size |
| Binary is unsigned (no Authenticode certificate) | Unknown publisher, higher threat score from SmartScreen | Open-source project, code signing not yet set up |
| Reads API keys from environment variables and sends them as HTTP headers | Matches credential harvesting behavior | Standard API authentication for LLM endpoints |
| Can run headlessly with no user interaction (`--yolo --prompt`) | Matches automated payload delivery | Designed for scripted/CI usage of the coding agent |
| Spawns sub-processes (subagents) that independently execute commands | Matches multi-stage dropper behavior | Subagent feature for parallel coding tasks |
| Accesses the clipboard | Common data exfiltration vector | Copy/paste support in the TUI |

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

These are planned improvements that may reduce or eliminate false positives:

- **Authenticode code signing** with an EV certificate to establish publisher trust.
- **Submitting the binary to Microsoft** via their false positive reporting portal for whitelisting.
- **Building natively on Windows** instead of cross-compiling from Linux, which can produce binaries with different internal structures that trigger entropy-based scanners.
- **Removing `-s -w` strip flags** from Windows release builds to preserve debug symbols.
