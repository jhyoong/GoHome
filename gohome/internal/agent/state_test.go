package agent

import (
	"errors"
	"sync"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/session"
)

func openTestWriter(t *testing.T) *session.Writer {
	t.Helper()
	w, err := session.OpenWriter(t.TempDir() + "/test.jsonl")
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func TestSwapWhileIdle(t *testing.T) {
	sess1 := session.NewSession("s1", t.TempDir(), "m", "ep")
	w1 := openTestWriter(t)
	st := NewSessionState(sess1, w1)

	sess2 := session.NewSession("s2", t.TempDir(), "m", "ep")
	w2 := openTestWriter(t)

	queued, err := st.Swap("resume s2", func() (*session.Session, *session.Writer, error) {
		return sess2, w2, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if queued {
		t.Fatal("expected immediate execution, got queued")
	}
	if st.Session().ID != "s2" {
		t.Errorf("session = %q, want s2", st.Session().ID)
	}
	if st.Writer() != w2 {
		t.Error("writer not updated")
	}
}

func TestSwapWhileBusy(t *testing.T) {
	sess1 := session.NewSession("s1", t.TempDir(), "m", "ep")
	w1 := openTestWriter(t)
	st := NewSessionState(sess1, w1)
	st.MarkBusy()

	sess2 := session.NewSession("s2", t.TempDir(), "m", "ep")
	w2 := openTestWriter(t)

	queued, err := st.Swap("resume s2", func() (*session.Session, *session.Writer, error) {
		return sess2, w2, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !queued {
		t.Fatal("expected queued, got immediate execution")
	}
	if st.Session().ID != "s1" {
		t.Errorf("session should still be s1 while busy, got %q", st.Session().ID)
	}
}

func TestDrainPendingExecutesSwap(t *testing.T) {
	sess1 := session.NewSession("s1", t.TempDir(), "m", "ep")
	w1 := openTestWriter(t)
	st := NewSessionState(sess1, w1)
	st.MarkBusy()

	sess2 := session.NewSession("s2", t.TempDir(), "m", "ep")
	w2 := openTestWriter(t)

	_, _ = st.Swap("resume s2", func() (*session.Session, *session.Writer, error) {
		return sess2, w2, nil
	})

	st.MarkIdle()

	tag, err := st.DrainPending()
	if err != nil {
		t.Fatalf("drain error: %v", err)
	}
	if tag != "resume s2" {
		t.Errorf("tag = %q, want %q", tag, "resume s2")
	}
	if st.Session().ID != "s2" {
		t.Errorf("session = %q, want s2", st.Session().ID)
	}
}

func TestDrainPendingNoop(t *testing.T) {
	sess1 := session.NewSession("s1", t.TempDir(), "m", "ep")
	w1 := openTestWriter(t)
	st := NewSessionState(sess1, w1)

	tag, err := st.DrainPending()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tag != "" {
		t.Errorf("expected empty tag, got %q", tag)
	}
}

func TestClearPendingDiscardsSwap(t *testing.T) {
	sess1 := session.NewSession("s1", t.TempDir(), "m", "ep")
	w1 := openTestWriter(t)
	st := NewSessionState(sess1, w1)
	st.MarkBusy()

	sess2 := session.NewSession("s2", t.TempDir(), "m", "ep")
	w2 := openTestWriter(t)

	_, _ = st.Swap("new", func() (*session.Session, *session.Writer, error) {
		return sess2, w2, nil
	})

	st.ClearPending()
	st.MarkIdle()

	tag, err := st.DrainPending()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tag != "" {
		t.Errorf("expected empty tag after clear, got %q", tag)
	}
	if st.Session().ID != "s1" {
		t.Error("session should remain s1 after clear")
	}
}

func TestSwapError(t *testing.T) {
	sess1 := session.NewSession("s1", t.TempDir(), "m", "ep")
	w1 := openTestWriter(t)
	st := NewSessionState(sess1, w1)

	_, err := st.Swap("bad", func() (*session.Session, *session.Writer, error) {
		return nil, nil, errors.New("writer open failed")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if st.Session().ID != "s1" {
		t.Error("session should remain s1 on swap error")
	}
}

func TestSetModelGuarded(t *testing.T) {
	sess1 := session.NewSession("s1", t.TempDir(), "m", "ep")
	w1 := openTestWriter(t)
	st := NewSessionState(sess1, w1)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st.SetModel("new-model")
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = st.Model()
		}()
	}
	wg.Wait()
}
