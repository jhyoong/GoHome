package headless

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
)

type Frontend struct {
	prompt  string
	verbose bool
	output  io.Writer
	sent    bool
	buf     strings.Builder
	mu      sync.Mutex
}

func NewFrontend(prompt string, verbose bool, output io.Writer) *Frontend {
	return &Frontend{
		prompt:  prompt,
		verbose: verbose,
		output:  output,
	}
}

func (f *Frontend) AwaitUserInput(ctx context.Context) (string, error) {
	f.mu.Lock()
	if !f.sent {
		f.sent = true
		f.mu.Unlock()
		return f.prompt, nil
	}
	f.mu.Unlock()
	<-ctx.Done()
	return "", ctx.Err()
}

func (f *Frontend) RequestApproval(_ context.Context, _ guard.ApprovalRequest) (guard.ApprovalDecision, error) {
	return guard.ApprovalDecision{Outcome: guard.AllowOnce}, nil
}

func (f *Frontend) Emit(_ string, ev agent.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.verbose {
		data, err := json.Marshal(ev)
		if err == nil {
			_, _ = f.output.Write(data)
			_, _ = f.output.Write([]byte("\n"))
		}
		return
	}

	if ev.Kind == agent.EventTokenDelta {
		f.buf.WriteString(ev.TextDelta)
	}
}

func (f *Frontend) FinalText() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.buf.String()
}
