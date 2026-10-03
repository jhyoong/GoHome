# Sudo pre-authentication design

## Problem

The sudo password dialog from #45 shows up and collects the password, but some
sudo commands still fail. Example:

```
sudo -n head -3 /etc/shadow
sudo: a password is required
```

Today `tools/shell.go` pipes the password into the shell's stdin and rewrites
`sudo` to `sudo -S` (`injectSudoS`). This fails when:

1. The model passes `-n`. `sudo -S -n` never reads a password, even when one is
   waiting on stdin. This is the reported bug.
2. The command pipes data into sudo (`echo x | sudo tee f`). Sudo reads the pipe
   instead of the password.
3. `sudo` appears on a later line. A newline is not a separator in the
   detection or rewrite regexes, so it is neither detected nor rewritten.

## Approach

Authenticate first, then run the command unchanged.

When a sudo password is present, the shell tool runs `sudo -S -v` in the same
shell, reading the password from fd 3. This caches sudo credentials for that
shell. Then the original command runs exactly as written. `sudo -n` succeeds
when credentials are cached, and stdin is free for the command.

Rejected alternatives:

- Patch the rewrite (strip `-n`, add newline as a separator): pipes into sudo
  and nested sudo calls stay broken.
- Askpass wrapper on PATH: `-n` still blocks askpass, and the password would
  have to sit in a temp file or env var the command could read.

## Components

### `internal/tools/shell.go` (Unix only)

- Remove `injectSudoS` and `sudoWordRe`.
- If `SudoPasswordFrom(ctx)` is non-empty:
  - Create an `os.Pipe`, write `password + "\n"`, close the write end.
  - Pass the read end as `cmd.ExtraFiles` (fd 3 in the child). Close the
    parent's copy after `cmd.Start`.
  - Leave `cmd.Stdin` unset.
  - Run this script instead of the raw command:

    ```sh
    sudo -S -v -p '' <&3 2>/dev/null || { echo "gohome: sudo password rejected" >&2; exit 1; }
    exec 3<&-
    <original command>
    exit $?
    ```

- `exec 3<&-` closes fd 3 so the user's command cannot read the password.
- The trailing `exit $?` keeps the original command from being the last
  command. Some shells exec the last command in place of themselves, which
  would change sudo's parent process and miss the cached credentials. With no
  terminal, sudo keys its credential cache by parent process ID.
- Windows is unchanged.

### `internal/guard/sudo.go`

Add newline and `(` to the separators in `sudoRe`. Sudo on a later line, or
inside `$(...)` or `( ... )`, then opens the password dialog.

### `internal/tui/model_approval.go`

`sudoRejected` also matches `gohome: sudo password rejected`, so a wrong
cached password is still cleared.

## Data flow and errors

- Correct password: `sudo -v` caches credentials, then the command runs. Works
  with `sudo`, `sudo -n`, `sudo -S`, pipes into sudo, and several sudo calls in
  one command.
- Wrong password: the command does not run. The result is `exit 1` and
  `gohome: sudo password rejected`. The TUI clears the cached password.
- NOPASSWD rules: `sudo -v` succeeds without reading fd 3.

Known limits (document in README):

- Sudo started by another program (`bash -c "sudo ..."`, `xargs sudo`,
  `find -exec sudo`) has a different parent process and may not see the
  cached credentials.
- A sudoers `timestamp_timeout=0` disables caching, so pre-authentication has
  no effect.

## Testing

- `internal/tools/shell_test.go`: replace the `injectSudoS` tests. Put a fake
  `sudo` script first on `PATH` that reads fd 3 and accepts one fixed
  password. Cover:
  - correct password runs the command
  - wrong password prints the marker and skips the command
  - the user command cannot read fd 3
  - stdin is not the password (`echo x | cat` style check)
  - no password in context leaves the command unchanged
- `internal/guard`: regex cases for newline, `$(sudo`, `(sudo`, and
  non-matches like `pseudo`.
- `internal/tui`: cached password plus the new marker clears the cache.
- Manual, on a Linux host with a sudo password:
  - `sudo -n head -3 /etc/shadow`
  - `echo x | sudo tee /tmp/gohome-sudo-test`
  - a two-line command with sudo on line 2
  - a wrong password
