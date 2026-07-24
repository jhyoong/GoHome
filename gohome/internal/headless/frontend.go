package headless

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

var ErrExit = errors.New("headless: exit requested")

type Frontend struct {
	prompt  string
	verbose bool
	output  io.Writer
	sent    bool
	buf     strings.Builder
	mu      sync.Mutex
	scanner *bufio.Scanner
}

func NewFrontend(prompt string, verbose bool, output io.Writer) *Frontend {
	return &Frontend{
		prompt:  prompt,
		verbose: verbose,
		output:  output,
	}
}

func NewInteractiveFrontend(input io.Reader, verbose bool, output io.Writer) *Frontend {
	return &Frontend{
		prompt:  "-",
		verbose: verbose,
		output:  output,
		scanner: bufio.NewScanner(input),
	}
}

func (f *Frontend) AwaitUserInput(ctx context.Context) (string, error) {
	if f.scanner != nil {
		return f.readStdin(ctx)
	}

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

func (f *Frontend) readStdin(_ context.Context) (string, error) {
	for f.scanner.Scan() {
		line := f.scanner.Text()
		if line == "" {
			continue
		}

		var envelope struct {
			Type    string `json:"type"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			f.emitWarning(fmt.Sprintf("invalid input: %v", err))
			continue
		}

		switch envelope.Type {
		case "user_message":
			return envelope.Content, nil
		case "exit":
			return "", ErrExit
		default:
			f.emitWarning(fmt.Sprintf("unknown input type: %q", envelope.Type))
			continue
		}
	}
	return "", ErrExit
}

func (f *Frontend) emitWarning(msg string) {
	data, err := session.EncodeEvent(session.Warning{Message: msg})
	if err != nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	_, _ = f.output.Write(data)
	_, _ = f.output.Write([]byte("\n"))
}

func (f *Frontend) FinalText() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.buf.String()
}
