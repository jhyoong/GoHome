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
	reader  *bufio.Reader
	// pending holds an in-flight stdin read abandoned by a cancelled ctx, so
	// the next call waits on it instead of starting a second reader.
	pending chan lineResult
	mainID  string
}

type lineResult struct {
	line []byte
	err  error
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
		reader:  bufio.NewReader(input),
	}
}

func (f *Frontend) AwaitUserInput(ctx context.Context) (string, error) {
	if f.reader != nil {
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

// SetMainSession sets the depth-0 session whose reply is printed. If it is
// never set, the first session to emit is taken as the main one.
func (f *Frontend) SetMainSession(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mainID = id
}

func (f *Frontend) Emit(sessionID string, ev agent.Event) {
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

	if f.mainID == "" {
		f.mainID = sessionID
	}
	if sessionID != f.mainID {
		return
	}

	// Each request starts a new reply, so only the final one is kept.
	switch ev.Kind {
	case agent.EventSending:
		f.buf.Reset()
	case agent.EventTokenDelta:
		f.buf.WriteString(ev.TextDelta)
	}
}

// readLine reads one line from stdin in a goroutine so ctx can interrupt the
// wait. A read abandoned by cancellation is picked up by the next call.
func (f *Frontend) readLine(ctx context.Context) ([]byte, error) {
	if f.pending == nil {
		ch := make(chan lineResult, 1)
		f.pending = ch
		go func() {
			line, err := f.reader.ReadBytes('\n')
			ch <- lineResult{line: line, err: err}
		}()
	}
	select {
	case r := <-f.pending:
		f.pending = nil
		return r.line, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *Frontend) readStdin(ctx context.Context) (string, error) {
	for {
		raw, rerr := f.readLine(ctx)
		if rerr != nil && !errors.Is(rerr, io.EOF) {
			if errors.Is(rerr, context.Canceled) || errors.Is(rerr, context.DeadlineExceeded) {
				return "", rerr
			}
			return "", fmt.Errorf("reading stdin: %w", rerr)
		}
		line := strings.TrimRight(string(raw), "\r\n")
		if line == "" {
			if rerr != nil {
				return "", ErrExit
			}
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
		}
		if rerr != nil {
			return "", ErrExit
		}
	}
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

// SteerCh implements agent.Frontend. The headless frontend does not support
// mid-turn steering, so it returns nil.
func (f *Frontend) SteerCh() <-chan string {
	return nil
}

func (f *Frontend) FinalText() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.buf.String()
}
