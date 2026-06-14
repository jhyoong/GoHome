# Fix /new and /resume Mutex Self-Deadlock — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Eliminate the self-deadlock that freezes the TUI when `/new` or `/resume` is executed, by changing the `Swap`/`DrainPending` closure signature to receive the old session and writer as arguments.

**Architecture:** The `SessionState.Swap` and `DrainPending` methods currently execute a caller-provided closure while holding `s.mu`. Callers pass closures that call `state.Writer()`, which re-acquires the same non-reentrant mutex, causing a deadlock. The fix changes the closure signature to `func(oldSess, oldWriter) (newSess, newWriter, error)` so `Swap`/`DrainPending` pass the guarded values directly and the closure never touches the mutex.

**Tech Stack:** Go, sync.Mutex, Bubble Tea TUI

---

### Task 1: Update `Swap` and `DrainPending` signatures in `state.go`

**Files:**
- Modify: `gohome/internal/agent/state.go:14-97`

**Step 1: Change the `pending` field type and `Swap` signature**

In `gohome/internal/agent/state.go`, change the `pending` field (line 20) and the `Swap` method (line 61) so the closure receives `(oldSess *session.Session, oldWriter *session.Writer)`:

```go
// state.go line 20 — change pending field type:
pending    func(oldSess *session.Session, oldWriter *session.Writer) (*session.Session, *session.Writer, error)

// state.go line 61 — change Swap signature:
func (s *SessionState) Swap(tag string, fn func(oldSess *session.Session, oldWriter *session.Writer) (*session.Session, *session.Writer, error)) (queued bool, err error) {

// state.go line 69 — pass s.sess and s.writer into fn:
	newSess, newWriter, err := fn(s.sess, s.writer)
```

**Step 2: Update `DrainPending` to pass old values into the closure**

In `gohome/internal/agent/state.go`, line 90 — pass `s.sess` and `s.writer` into `fn`:

```go
	newSess, newWriter, err := fn(s.sess, s.writer)
```

**Step 3: Run vet to confirm compilation**

Run: `go vet ./gohome/internal/agent/`
Expected: Compilation errors in tests and main.go (callers still use old signature). That is expected at this step — we fix callers next.

**Step 4: Commit**

```bash
git add gohome/internal/agent/state.go
git commit -m "fix: change Swap/DrainPending closure signature to receive old session and writer

Eliminates self-deadlock where closures called state.Writer() while
Swap already held the mutex."
```

---

### Task 2: Update `state_test.go` closures

**Files:**
- Modify: `gohome/internal/agent/state_test.go`

**Step 1: Update all closure signatures in tests**

Every closure passed to `Swap` in this file must change from `func() (...)` to `func(oldSess *session.Session, oldWriter *session.Writer) (...)`. The closures in these tests don't use the old values — they just need the parameter list to match. Update all five occurrences:

- Line 41 (`TestSwapWhileIdle`)
- Line 67 (`TestSwapWhileBusy`)
- Line 90 (`TestDrainPendingExecutesSwap`)
- Line 131 (`TestClearPendingDiscardsSwap`)
- Line 155 (`TestSwapError`)

Example — `TestSwapWhileIdle` line 41:

```go
	queued, err := st.Swap("resume s2", func(oldSess *session.Session, oldWriter *session.Writer) (*session.Session, *session.Writer, error) {
		return sess2, w2, nil
	})
```

Apply the same pattern to all five. Use `_` for unused params where the test doesn't reference them:

```go
	func(_ *session.Session, _ *session.Writer) (*session.Session, *session.Writer, error) {
```

**Step 2: Run tests**

Run: `go test ./gohome/internal/agent/ -run 'TestSwap|TestDrain|TestClear' -v`
Expected: All 5 tests PASS.

**Step 3: Commit**

```bash
git add gohome/internal/agent/state_test.go
git commit -m "test: update state_test.go closures for new Swap signature"
```

---

### Task 3: Update `main.go` closures — fix the deadlock

**Files:**
- Modify: `gohome/cmd/gohome/main.go:395-439`

**Step 1: Update the `ResumeSession` closure (line 395)**

Change from:
```go
queued, err := state.Swap("resume "+id, func() (*session.Session, *session.Writer, error) {
    oldWriter := state.Writer()
```

To:
```go
queued, err := state.Swap("resume "+id, func(_ *session.Session, oldWriter *session.Writer) (*session.Session, *session.Writer, error) {
```

Remove the `oldWriter := state.Writer()` line entirely — `oldWriter` now comes from the parameter.

**Step 2: Update the `NewSession` closure (line 417)**

Change from:
```go
queued, err := state.Swap("new "+id, func() (*session.Session, *session.Writer, error) {
    oldWriter := state.Writer()
```

To:
```go
queued, err := state.Swap("new "+id, func(_ *session.Session, oldWriter *session.Writer) (*session.Session, *session.Writer, error) {
```

Remove the `oldWriter := state.Writer()` line entirely.

**Step 3: Run vet**

Run: `go vet ./gohome/cmd/gohome/`
Expected: Compilation errors in `main_test.go` only (we fix that next).

**Step 4: Commit**

```bash
git add gohome/cmd/gohome/main.go
git commit -m "fix: use Swap-provided oldWriter in /new and /resume closures

This eliminates the mutex self-deadlock that froze the TUI."
```

---

### Task 4: Update `main_test.go` closure

**Files:**
- Modify: `gohome/cmd/gohome/main_test.go:242`

**Step 1: Update `TestConcurrentSwapAndRun` closure (line 242)**

Change from:
```go
queued, err := state.Swap("new swapped", func() (*session.Session, *session.Writer, error) {
```

To:
```go
queued, err := state.Swap("new swapped", func(_ *session.Session, _ *session.Writer) (*session.Session, *session.Writer, error) {
```

**Step 2: Run all tests**

Run: `go test ./gohome/... -count=1`
Expected: All tests PASS.

**Step 3: Run lint**

Run: `golangci-lint run ./gohome/...`
Expected: No new warnings.

**Step 4: Commit**

```bash
git add gohome/cmd/gohome/main_test.go
git commit -m "test: update main_test.go closure for new Swap signature"
```

---

### Task 5: Manual verification

**Step 1: Build**

Run: `go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome`
Expected: Binary builds successfully.

**Step 2: Run the binary and test /new**

Run: `./bin/gohome --model <your-model-config>`
Then type `/new` and press Enter.
Expected: New session created, status bar shows "New session: <id>", TUI remains responsive.

**Step 3: Run full test suite one more time**

Run: `go test ./gohome/... -count=1 -race`
Expected: All tests PASS with no data races.
