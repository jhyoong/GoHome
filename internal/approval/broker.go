package approval

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/JiaHui/gohome/internal/config"
)

var ErrApprovalTimeout = errors.New("approval timed out")

type Request struct {
	ID     string
	Tool   string
	Params json.RawMessage
}

type Broker struct {
	cfg     config.ApprovalConfig
	send    chan<- Request
	mu      sync.Mutex
	pending map[string]chan bool
}

func NewBroker(cfg config.ApprovalConfig, send chan<- Request) *Broker {
	return &Broker{
		cfg:     cfg,
		send:    send,
		pending: make(map[string]chan bool),
	}
}

func (b *Broker) Request(ctx context.Context, id, tool string, params json.RawMessage) (bool, error) {
	for _, entry := range b.cfg.Whitelist {
		if entry.Tool == tool {
			switch entry.Allow {
			case "always":
				return true, nil
			case "never":
				return false, nil
			}
		}
	}

	if b.cfg.AutoApproveAll {
		return true, nil
	}

	ch := make(chan bool, 1)
	b.mu.Lock()
	b.pending[id] = ch
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
	}()

	if b.send != nil {
		select {
		case b.send <- Request{ID: id, Tool: tool, Params: params}:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}

	timeout := time.Duration(b.cfg.DefaultTimeout) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	select {
	case decision := <-ch:
		return decision, nil
	case <-ctx.Done():
		return false, ctx.Err()
	case <-time.After(timeout):
		return false, ErrApprovalTimeout
	}
}

func (b *Broker) Respond(id string, approved bool) {
	b.mu.Lock()
	ch, ok := b.pending[id]
	b.mu.Unlock()
	if ok {
		ch <- approved
	}
}
