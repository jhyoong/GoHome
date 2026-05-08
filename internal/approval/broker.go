package approval

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/jhyoong/gohome/internal/config"
)

var ErrApprovalTimeout = errors.New("approval timed out")

type Request struct {
	ID     string
	Tool   string
	Params json.RawMessage
}

type Broker struct {
	autoApproveAll bool
	defaultTimeout int
	send           chan<- Request

	mu      sync.Mutex // protects pending
	pending map[string]chan bool

	wlMu      sync.RWMutex // protects whitelist
	whitelist []config.WhitelistEntry
}

func NewBroker(cfg config.ApprovalConfig, send chan<- Request) *Broker {
	wl := make([]config.WhitelistEntry, len(cfg.Whitelist))
	copy(wl, cfg.Whitelist)
	return &Broker{
		autoApproveAll: cfg.AutoApproveAll,
		defaultTimeout: cfg.DefaultTimeout,
		send:           send,
		pending:        make(map[string]chan bool),
		whitelist:      wl,
	}
}

// AddWhitelistEntry appends an entry to the in-memory whitelist.
// Safe to call from multiple goroutines.
func (b *Broker) AddWhitelistEntry(entry config.WhitelistEntry) {
	b.wlMu.Lock()
	defer b.wlMu.Unlock()
	b.whitelist = append(b.whitelist, entry)
}

func (b *Broker) Request(ctx context.Context, id, tool string, params json.RawMessage) (bool, error) {
	b.wlMu.RLock()
	whitelist := make([]config.WhitelistEntry, len(b.whitelist))
	copy(whitelist, b.whitelist)
	b.wlMu.RUnlock()

	if tool == "shell" {
		cmd := extractShellCommand(params)
		chained := isChainedCommand(cmd)
		for _, entry := range whitelist {
			if entry.Tool != "shell" {
				continue
			}
			// No CommandPattern means match all (backward compatibility).
			matches := entry.CommandPattern == "" || matchGlob(entry.CommandPattern, cmd)
			if !matches {
				continue
			}
			switch entry.Allow {
			case "never":
				return false, nil // "never" applies even to chained commands
			case "always":
				if !chained {
					return true, nil
				}
				// chained + "always" → fall through to approval
			}
		}
	} else {
		for _, entry := range whitelist {
			if entry.Tool == tool {
				switch entry.Allow {
				case "always":
					return true, nil
				case "never":
					return false, nil
				}
			}
		}
	}

	if b.autoApproveAll {
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

	timeout := time.Duration(b.defaultTimeout) * time.Second
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
