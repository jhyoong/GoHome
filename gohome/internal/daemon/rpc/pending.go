package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// pendingResult holds the outcome of an in-flight JSON-RPC request.
type pendingResult struct {
	data json.RawMessage
	err  *Error
}

// Pending tracks in-flight JSON-RPC requests and correlates responses by ID.
// Both daemon and TUI sides use this to match outgoing requests with their
// eventual responses.
type Pending struct {
	mu    sync.Mutex
	calls map[int64]chan pendingResult
}

// NewPending creates a new Pending tracker.
func NewPending() *Pending {
	return &Pending{
		calls: make(map[int64]chan pendingResult),
	}
}

// Call registers an in-flight request with the given id and blocks until
// Resolve is called with the matching id or ctx is cancelled. On RPC error
// it returns a formatted error. The channel is cleaned up on return.
func (p *Pending) Call(ctx context.Context, id int64) (json.RawMessage, error) {
	p.Register(id)
	return p.Wait(ctx, id)
}

// Register creates a channel for the given id so that a subsequent Resolve
// can deliver a result even before Wait is called. This avoids the race where
// a response arrives between writing the request and calling Wait.
func (p *Pending) Register(id int64) {
	p.mu.Lock()
	p.calls[id] = make(chan pendingResult, 1)
	p.mu.Unlock()
}

// Wait blocks until Resolve delivers a result for the given id or ctx is
// cancelled. The channel is cleaned up on return. Register must be called
// before Wait.
func (p *Pending) Wait(ctx context.Context, id int64) (json.RawMessage, error) {
	p.mu.Lock()
	ch := p.calls[id]
	p.mu.Unlock()

	if ch == nil {
		return nil, fmt.Errorf("pending: no registered call for id %d", id)
	}

	defer func() {
		p.mu.Lock()
		delete(p.calls, id)
		p.mu.Unlock()
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.err != nil {
			return nil, fmt.Errorf("rpc error %d: %s", res.err.Code, res.err.Message)
		}
		return res.data, nil
	}
}

// Cancel removes a registered call without delivering a result. Use this to
// clean up after Register if the write fails before Wait is called.
func (p *Pending) Cancel(id int64) {
	p.mu.Lock()
	delete(p.calls, id)
	p.mu.Unlock()
}

// Resolve delivers a response to a waiting Call with the matching id. If no
// call is pending for the given id, the response is silently dropped.
func (p *Pending) Resolve(id int64, result json.RawMessage, rpcErr *Error) {
	p.mu.Lock()
	ch, ok := p.calls[id]
	p.mu.Unlock()

	if !ok {
		return
	}

	ch <- pendingResult{data: result, err: rpcErr}
}
