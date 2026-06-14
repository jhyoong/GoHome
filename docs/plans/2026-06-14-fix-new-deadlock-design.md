# Fix /new and /resume mutex self-deadlock

## Problem

The `/new` and `/resume` slash commands freeze the TUI immediately on execution.

### Root cause

`SessionState.Swap()` and `DrainPending()` execute the caller-provided closure while
holding `s.mu`. The closures registered in `main.go` call `state.Writer()`, which
tries to re-acquire the same non-reentrant `sync.Mutex`. This is a self-deadlock.

The deadlock occurs in the TUI goroutine (inside Bubble Tea's `Update`), so the
entire UI becomes unresponsive.

**Affected paths:**
- `/new` callback (`main.go:412`) -- `state.Writer()` inside Swap closure
- `/resume` callback (`main.go:395`) -- `state.Writer()` inside Swap closure
- `DrainPending` (`state.go:80`) -- executes queued closures under the same lock

## Fix

Change the `Swap` and `DrainPending` closure signature to receive the current
session and writer as arguments, so closures never need to call lock-guarded
methods on `SessionState`.

### Before

```go
type pendingFn = func() (*session.Session, *session.Writer, error)
```

### After

```go
type pendingFn = func(oldSess *session.Session, oldWriter *session.Writer) (*session.Session, *session.Writer, error)
```

### Files to change

1. **`gohome/internal/agent/state.go`** -- Update `Swap`, `DrainPending`, and
   the `pending` field to use the new signature. Pass `s.sess` and `s.writer`
   into the closure call.

2. **`gohome/cmd/gohome/main.go`** -- Update `NewSession` and `ResumeSession`
   closures to accept `(oldSess, oldWriter)` args instead of calling
   `state.Writer()`.

3. **`gohome/internal/agent/state_test.go`** -- Update all test closures to
   accept the new `(oldSess, oldWriter)` parameters.

4. **`gohome/cmd/gohome/main_test.go`** -- Update `TestConcurrentSwapAndRun`
   closure signature.
