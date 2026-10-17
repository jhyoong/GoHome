package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

// doneClient answers every request with an empty end_turn and records the
// requests it was sent.
type doneClient struct {
	mu   sync.Mutex
	reqs []common.Request
}

func (c *doneClient) Stream(_ context.Context, req common.Request) (<-chan common.StreamEvent, error) {
	c.mu.Lock()
	c.reqs = append(c.reqs, req)
	c.mu.Unlock()
	ch := make(chan common.StreamEvent, 1)
	ch <- common.StreamEvent{Kind: common.EventTurnDone, StopReason: "end_turn"}
	close(ch)
	return ch, nil
}

func (c *doneClient) last() common.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reqs[len(c.reqs)-1]
}

// TestModelSwitchDuringTurn_NoRace switches the model (as /model does) while
// turns run. Run with -race.
func TestModelSwitchDuringTurn_NoRace(t *testing.T) {
	client := &doneClient{}
	a, sess, _ := newTestAgent(t, client, &fakeRecorder{})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			a.State.SetModelSettings(ModelSettings{Name: "new-model", MaxTokens: 100})
		}
	}()
	for i := 0; i < 100; i++ {
		if _, _, err := a.Turn(context.Background(), sess); err != nil {
			t.Fatalf("Turn: %v", err)
		}
	}
	<-done
}

// TestResumeSwap_UsesCurrentModel resumes a session saved with an old model
// and checks the next turn sends the current model.
func TestResumeSwap_UsesCurrentModel(t *testing.T) {
	client := &doneClient{}
	a, _, _ := newTestAgent(t, client, &fakeRecorder{})
	current := a.State.Model()

	resumed := session.NewSession("old-sess", t.TempDir(), "old-model", "old-cfg")
	err := a.State.Swap("resume old-sess", func(_ *session.Session, w *session.Writer) (*session.Session, *session.Writer, error) {
		return resumed, w, nil
	})
	if err != nil {
		t.Fatalf("Swap: %v", err)
	}

	if _, _, err := a.Turn(context.Background(), a.State.Session()); err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if got := client.last().Model; got != current {
		t.Errorf("request model = %q, want %q", got, current)
	}
	if resumed.Model != current || resumed.ModelConfig != "anthropic" {
		t.Errorf("resumed session model = %q/%q, want %q/anthropic", resumed.Model, resumed.ModelConfig, current)
	}
}

// TestSpawn_ChildUsesActiveModelSettings checks a subagent sends the
// parent's active model and limits.
func TestSpawn_ChildUsesActiveModelSettings(t *testing.T) {
	client := &doneClient{}
	a := buildSpawnParent(t, client, t.TempDir())
	want := ModelSettings{Name: "switched", ConfigName: "cfg2", MaxTokens: 777, ThinkingBudget: 55, ReasoningEffort: "low"}
	a.State.SetModelSettings(want)

	if _, _, err := a.Spawn(context.Background(), "task", ""); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	req := client.last()
	if req.Model != want.Name || req.MaxTokens != want.MaxTokens ||
		req.ThinkingBudget != want.ThinkingBudget || req.ReasoningEffort != want.ReasoningEffort {
		t.Errorf("child request = %q/%d/%d/%q, want %+v", req.Model, req.MaxTokens, req.ThinkingBudget, req.ReasoningEffort, want)
	}
}
