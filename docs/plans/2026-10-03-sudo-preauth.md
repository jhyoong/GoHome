# Sudo Pre-authentication Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make sudo commands work with the password from the TUI dialog, including `sudo -n`, pipes into sudo, and sudo on later lines.

**Architecture:** When a sudo password is in the context, the shell tool runs `sudo -S -v` first, reading the password from fd 3, then runs the original command unchanged in the same shell. The stdin rewrite (`injectSudoS`) is removed. Sudo detection also treats newline and `(` as command boundaries. Design: `docs/plans/2026-10-03-sudo-preauth-design.md`.

**Tech Stack:** Go 1.25, `os/exec` with `ExtraFiles`, `/bin/sh`.

**Branch:** `fix/sudo-preauth` (already created; the design doc is committed there).

---

### Task 1: Detect sudo after newline and `(`

**Files:**
- Modify: `gohome/internal/guard/sudo.go:5-12`
- Test: `gohome/internal/guard/sudo_test.go`

**Step 1: Write the failing test**

Add these rows to the `tests` table in `TestIsSudoCommand`:

```go
		{"sudo on second line", "echo hi\nsudo ls", true},
		{"sudo in command substitution", "x=$(sudo cat /etc/shadow)", true},
		{"sudo in subshell", "(sudo ls)", true},
		{"sudo after newline and spaces", "echo hi\n  sudo ls", true},
		{"pseudo in subshell", "(pseudo ls)", false},
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/guard/ -run TestIsSudoCommand -v`
Expected: FAIL on `sudo_on_second_line`, `sudo_in_command_substitution`, `sudo_in_subshell`, `sudo_after_newline_and_spaces`.

**Step 3: Implement**

Replace the regex and comment in `gohome/internal/guard/sudo.go`:

```go
var sudoRe = regexp.MustCompile(`(^|[;&|(\n]\s*)sudo(\s|$)`)

// IsSudoCommand reports whether the given shell command invokes sudo.
// It matches sudo at the start of the command, after shell operators
// (;, &, &&, ||, |), after an opening parenthesis (subshells and $(...)),
// or at the start of a later line, but not as a substring of another word.
func IsSudoCommand(command string) bool {
	return sudoRe.MatchString(command)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/guard/ -v -run 'TestIsSudoCommand|TestCheck_'`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/guard/sudo.go gohome/internal/guard/sudo_test.go
git commit -m "fix(guard): detect sudo after newline and opening parenthesis"
```

---

### Task 2: Pre-authenticate sudo in the shell tool

**Files:**
- Modify: `gohome/internal/tools/shell.go` (imports, lines 61-77, lines 103-124, cmd start)
- Test: `gohome/internal/tools/shell_test.go` (replace lines 154-212)

**Step 1: Write the failing tests**

In `gohome/internal/tools/shell_test.go`, delete `TestBash_SudoPasswordPipedToStdin`, `TestBash_NoSudoPassword_StdinEmpty`, and `TestInjectSudoS`. Add `"os"` and `"path/filepath"` to the imports. Then add:

```go
// installFakeSudo puts a fake "sudo" first on PATH. "sudo -S -v -p ''"
// reads one line from stdin and accepts only "goodpass". Any other call
// prints "fake-sudo:" and its arguments, so tests can see it ran.
func installFakeSudo(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
if [ "$1" = "-S" ] && [ "$2" = "-v" ]; then
	read -r pw
	if [ "$pw" = "goodpass" ]; then exit 0; fi
	echo "Sorry, try again." >&2
	exit 1
fi
echo "fake-sudo: $*"
`
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake sudo: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func execBashWithSudo(t *testing.T, password, command string) Result {
	t.Helper()
	ctx := WithSudoPassword(context.Background(), password)
	raw, _ := json.Marshal(map[string]any{"command": command})
	res, err := (&ShellTool{}).Execute(ctx, raw, NullSink{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return res
}

func TestBash_SudoPreauth_CorrectPasswordRunsCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	installFakeSudo(t)
	res := execBashWithSudo(t, "goodpass", "sudo -n head -3 /etc/shadow")
	if !strings.HasPrefix(res.Content, "exit 0\n") {
		t.Errorf("want exit 0, got %q", res.Content)
	}
	if !strings.Contains(res.Content, "fake-sudo: -n head -3 /etc/shadow") {
		t.Errorf("command should run unchanged, got %q", res.Content)
	}
}

func TestBash_SudoPreauth_WrongPasswordSkipsCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	installFakeSudo(t)
	res := execBashWithSudo(t, "badpass", "echo ran")
	if !strings.HasPrefix(res.Content, "exit 1\n") {
		t.Errorf("want exit 1, got %q", res.Content)
	}
	if !strings.Contains(res.Content, SudoRejectedMarker) {
		t.Errorf("want rejection marker, got %q", res.Content)
	}
	if strings.Contains(res.Content, "ran") {
		t.Errorf("command must not run after rejection, got %q", res.Content)
	}
}

func TestBash_SudoPreauth_PasswordNotReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	installFakeSudo(t)
	// fd 3 is closed before the command; stdin is empty.
	res := execBashWithSudo(t, "goodpass", "cat <&3 2>/dev/null; cat")
	if strings.Contains(res.Content, "goodpass") {
		t.Errorf("password leaked to command, got %q", res.Content)
	}
}

func TestBash_SudoPreauth_PipeIntoSudo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	installFakeSudo(t)
	res := execBashWithSudo(t, "goodpass", "echo data | sudo tee /tmp/x")
	if !strings.Contains(res.Content, "fake-sudo: tee /tmp/x") {
		t.Errorf("want sudo to run without -S rewrite, got %q", res.Content)
	}
}

func TestBash_SudoPreauth_ExitCodePreserved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	installFakeSudo(t)
	res := execBashWithSudo(t, "goodpass", "echo a\nsh -c 'exit 7'")
	if !strings.HasPrefix(res.Content, "exit 7\n") {
		t.Errorf("want exit 7, got %q", res.Content)
	}
}

func TestBash_NoSudoPassword_CommandUnchanged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	installFakeSudo(t)
	res := execBash(t, map[string]any{"command": "sudo ls"})
	if !strings.Contains(res.Content, "fake-sudo: ls") {
		t.Errorf("want plain sudo call, got %q", res.Content)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/tools/ -run 'TestBash_SudoPreauth|TestBash_NoSudoPassword' -v`
Expected: build failure, `undefined: SudoRejectedMarker`.

**Step 3: Implement**

In `gohome/internal/tools/shell.go`:

1. Imports: add `"os"`, remove `"regexp"`.

2. Replace `sudoWordRe` and `injectSudoS` (lines 61-77) with:

```go
// SudoRejectedMarker is printed when sudo rejects the password during
// pre-authentication. The TUI matches it to clear a cached password.
const SudoRejectedMarker = "gohome: sudo password rejected"

// wrapSudoPreauth returns a script that validates the sudo password read
// from fd 3, closes fd 3, then runs command unchanged. With no terminal,
// sudo caches credentials per parent process, so later sudo calls from
// this shell (including "sudo -n") succeed without a password. The final
// "exit $?" keeps the shell from exec-ing the last command in place of
// itself, which would change sudo's parent process.
func wrapSudoPreauth(command string) string {
	return "sudo -S -v -p '' <&3 2>/dev/null || { echo '" + SudoRejectedMarker + "' >&2; exit 1; }\n" +
		"exec 3<&-\n" +
		command + "\n" +
		"exit $?\n"
}

// sudoPasswordPipe returns the read end of a pipe that holds password
// followed by a newline. The caller must close it.
func sudoPasswordPipe(password string) (*os.File, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	_, err = w.WriteString(password + "\n")
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = r.Close()
		return nil, err
	}
	return r, nil
}
```

3. Replace the command construction and the old stdin block (lines 103-124, from `var cmd *exec.Cmd` through the closing `}` of `if sudoPassword != ""`) with:

```go
	script := inp.Command
	var passwordFile *os.File
	if pw := SudoPasswordFrom(ctx); pw != "" && runtime.GOOS != "windows" {
		f, err := sudoPasswordPipe(pw)
		if err != nil {
			return Result{IsError: true, Content: "shell: sudo password pipe: " + err.Error()}, nil
		}
		defer f.Close()
		passwordFile = f
		script = wrapSudoPreauth(inp.Command)
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", script)
	}
	detachFromTerminal(cmd)

	if inp.CWD != nil {
		cmd.Dir = *inp.CWD
	}

	// The password reaches the child as fd 3, never as stdin.
	if passwordFile != nil {
		cmd.ExtraFiles = []*os.File{passwordFile}
	}
```

`defer f.Close()` closes the parent's copy when `Execute` returns. The child keeps its own copy until the script runs `exec 3<&-`.

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/tools/ -v -run 'TestBash'`
Expected: PASS

Run: `go vet ./gohome/internal/tools/`
Expected: no output

**Step 5: Commit**

```bash
git add gohome/internal/tools/shell.go gohome/internal/tools/shell_test.go
git commit -m "fix(tools): pre-authenticate sudo via fd 3 instead of stdin rewrite"
```

---

### Task 3: Clear cached password on the new rejection marker

**Files:**
- Modify: `gohome/internal/tui/model_approval.go:189-192`
- Test: `gohome/internal/tui/sudo_prompt_test.go` (`TestSudoRejected`, around line 496)

`tui` does not import `tools`, so the marker string is repeated here with a comment pointing at its source.

**Step 1: Write the failing test**

Add to the `cases` map in `TestSudoRejected`:

```go
		"exit 1\ngohome: sudo password rejected\n": true,
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/tui/ -run TestSudoRejected -v`
Expected: FAIL, `sudoRejected("exit 1\ngohome: sudo password rejected\n") = false, want true`

**Step 3: Implement**

Replace `sudoRejected` in `gohome/internal/tui/model_approval.go`:

```go
// sudoRejected reports whether shell output shows sudo rejecting the
// password. "gohome: sudo password rejected" matches
// tools.SudoRejectedMarker.
func sudoRejected(output string) bool {
	return strings.Contains(output, "gohome: sudo password rejected") ||
		strings.Contains(output, "Sorry, try again") ||
		strings.Contains(output, "incorrect password attempt")
}
```

Keep any existing doc comment text above the function if it says more than this; only add the marker sentence.

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/tui/ -v -run 'TestSudo'`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/tui/model_approval.go gohome/internal/tui/sudo_prompt_test.go
git commit -m "fix(tui): clear cached sudo password on pre-auth rejection"
```

---

### Task 4: Document how sudo runs and its limits

**Files:**
- Modify: `README.md:293` and `README.md:301` (section "Sudo commands")

**Step 1: Edit README**

After the paragraph at line 293, add:

```markdown
When you enter a password, `gohome` first checks it with `sudo -v` in the same shell, then runs the command exactly as written. The password goes to sudo on a separate file descriptor, never on stdin, so commands such as `sudo -n ...` and `echo x | sudo tee file` work. If sudo rejects the password, the command does not run and the output says `gohome: sudo password rejected`.

Limits: sudo started by another program (`bash -c "sudo ..."`, `xargs sudo`, `find -exec sudo`) may not see the checked password and can fail. A sudoers setting of `timestamp_timeout=0` turns off this check entirely.
```

In the paragraph at line 301, change `If sudo rejects a cached password ("Sorry, try again")` to `If sudo rejects a cached password`.

**Step 2: Commit**

```bash
git add README.md
git commit -m "docs: describe sudo pre-authentication and its limits"
```

---

### Task 5: Full verification

**Step 1: Run the full checks**

Run: `go vet ./gohome/... && go test ./gohome/...`
Expected: all packages `ok`

Run: `golangci-lint run ./gohome/...`
Expected: no issues

Run: `go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome`
Expected: builds with no output

**Step 2: Manual check on a Linux host with a sudo password**

Run `./bin/gohome` on the host and ask the agent to run each command below. Enter the real password in the dialog.

1. `sudo -n head -3 /etc/shadow`: prints the first 3 lines, `exit 0`.
2. `echo hello | sudo tee /tmp/gohome-sudo-test`: prints `hello`, `exit 0`. Then `cat /tmp/gohome-sudo-test` shows `hello`.
3. A two-line command: `echo first` on line 1, `sudo -n id -u` on line 2: dialog appears, prints `0`.
4. Any sudo command with a wrong password: `exit 1` and `gohome: sudo password rejected`; the command does not run.
5. With `"cacheSudoPassword": true`, repeat step 4 after a correct cached password is replaced by a wrong one: notice "Cached sudo password was rejected and cleared".

Record the results in the PR description. If step 1 fails while step 4 behaves correctly, the host's sudo does not share credentials between `sudo -v` and the command. Stop and report back before changing the design.
