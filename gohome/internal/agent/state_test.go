package agent

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

type fakeStateClient struct {
	name string
}

func (c *fakeStateClient) Stream(_ context.Context, _ common.Request) (<-chan common.StreamEvent, error) {
	ch := make(chan common.StreamEvent)
	close(ch)
	return ch, nil
}

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
	st := NewSessionState(sess1, w1, nil)

	sess2 := session.NewSession("s2", t.TempDir(), "m", "ep")
	w2 := openTestWriter(t)

	queued, err := st.Swap("resume s2", func(_ *session.Session, _ *session.Writer) (*session.Session, *session.Writer, error) {
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
	st := NewSessionState(sess1, w1, nil)
	st.MarkBusy()

	sess2 := session.NewSession("s2", t.TempDir(), "m", "ep")
	w2 := openTestWriter(t)

	queued, err := st.Swap("resume s2", func(_ *session.Session, _ *session.Writer) (*session.Session, *session.Writer, error) {
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
	st := NewSessionState(sess1, w1, nil)
	st.MarkBusy()

	sess2 := session.NewSession("s2", t.TempDir(), "m", "ep")
	w2 := openTestWriter(t)

	_, _ = st.Swap("resume s2", func(_ *session.Session, _ *session.Writer) (*session.Session, *session.Writer, error) {
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
	st := NewSessionState(sess1, w1, nil)

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
	st := NewSessionState(sess1, w1, nil)
	st.MarkBusy()

	sess2 := session.NewSession("s2", t.TempDir(), "m", "ep")
	w2 := openTestWriter(t)

	_, _ = st.Swap("new", func(_ *session.Session, _ *session.Writer) (*session.Session, *session.Writer, error) {
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
	st := NewSessionState(sess1, w1, nil)

	_, err := st.Swap("bad", func(_ *session.Session, _ *session.Writer) (*session.Session, *session.Writer, error) {
		return nil, nil, errors.New("writer open failed")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if st.Session().ID != "s1" {
		t.Error("session should remain s1 on swap error")
	}
}

func TestSwapClosureUsesOldWriter(t *testing.T) {
	sess1 := session.NewSession("s1", t.TempDir(), "m", "ep")
	w1 := openTestWriter(t)
	st := NewSessionState(sess1, w1, nil)

	sess2 := session.NewSession("s2", t.TempDir(), "m", "ep")
	w2 := openTestWriter(t)

	var capturedWriter *session.Writer
	queued, err := st.Swap("new s2", func(_ *session.Session, oldWriter *session.Writer) (*session.Session, *session.Writer, error) {
		capturedWriter = oldWriter
		oldWriter.Emit(session.SessionEnd{Reason: "switch"})
		return sess2, w2, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if queued {
		t.Fatal("expected immediate execution, got queued")
	}
	if capturedWriter != w1 {
		t.Error("closure did not receive the old writer")
	}
	if st.Writer() != w2 {
		t.Error("writer not updated after swap")
	}
}

func TestDrainPendingClosureUsesOldWriter(t *testing.T) {
	sess1 := session.NewSession("s1", t.TempDir(), "m", "ep")
	w1 := openTestWriter(t)
	st := NewSessionState(sess1, w1, nil)
	st.MarkBusy()

	sess2 := session.NewSession("s2", t.TempDir(), "m", "ep")
	w2 := openTestWriter(t)

	var capturedWriter *session.Writer
	_, _ = st.Swap("new s2", func(_ *session.Session, oldWriter *session.Writer) (*session.Session, *session.Writer, error) {
		capturedWriter = oldWriter
		oldWriter.Emit(session.SessionEnd{Reason: "switch"})
		return sess2, w2, nil
	})

	st.MarkIdle()
	tag, err := st.DrainPending()
	if err != nil {
		t.Fatalf("drain error: %v", err)
	}
	if tag != "new s2" {
		t.Errorf("tag = %q, want %q", tag, "new s2")
	}
	if capturedWriter != w1 {
		t.Error("closure did not receive the old writer via DrainPending")
	}
	if st.Writer() != w2 {
		t.Error("writer not updated after drain")
	}
}

func TestSetModelConfigGuarded(t *testing.T) {
	sess := session.NewSession("s1", t.TempDir(), "m", "ep")
	w := openTestWriter(t)
	st := NewSessionState(sess, w, nil)

	st.SetModelConfig("cfg-new")
	if got := st.Session().ModelConfig; got != "cfg-new" {
		t.Errorf("ModelConfig = %q, want cfg-new", got)
	}
}

func TestClientGetterSetter(t *testing.T) {
	sess := session.NewSession("s1", t.TempDir(), "m", "ep")
	w := openTestWriter(t)

	fakeC := &fakeStateClient{name: "initial"}
	st := NewSessionState(sess, w, fakeC)

	if st.Client() != fakeC {
		t.Error("Client() did not return initial client")
	}

	fakeC2 := &fakeStateClient{name: "swapped"}
	st.SetClient(fakeC2)

	if st.Client() != fakeC2 {
		t.Error("Client() did not return swapped client after SetClient")
	}
}

func TestSetClientGuarded(t *testing.T) {
	sess := session.NewSession("s1", t.TempDir(), "m", "ep")
	w := openTestWriter(t)
	st := NewSessionState(sess, w, nil)

	c1 := &fakeStateClient{name: "c1"}
	c2 := &fakeStateClient{name: "c2"}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			st.SetClient(c1)
		}()
		go func() {
			defer wg.Done()
			_ = st.Client()
		}()
	}
	wg.Wait()

	got := st.Client()
	if got != c1 && got != c2 {
		t.Error("Client() returned unexpected value after concurrent access")
	}
	_ = c2
}

func TestSetModelGuarded(t *testing.T) {
	sess1 := session.NewSession("s1", t.TempDir(), "m", "ep")
	w1 := openTestWriter(t)
	st := NewSessionState(sess1, w1, nil)

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
