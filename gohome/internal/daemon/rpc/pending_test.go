package rpc

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestPending_RegisterWaitAndResolve(t *testing.T) {
	p := NewPending()

	var got json.RawMessage
	var gotErr error
	done := make(chan struct{})

	p.Register(42)
	go func() {
		got, gotErr = p.Wait(context.Background(), 42)
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	p.Resolve(42, json.RawMessage(`{"ok":true}`), nil)

	<-done

	if gotErr != nil {
		t.Fatalf("expected no error, got %v", gotErr)
	}

	want := `{"ok":true}`
	if string(got) != want {
		t.Fatalf("result = %s, want %s", got, want)
	}
}

func TestPending_WaitCancelled(t *testing.T) {
	p := NewPending()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	p.Register(99)
	_, err := p.Wait(ctx, 99)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

func TestPending_ResolveWithError(t *testing.T) {
	p := NewPending()

	var gotErr error
	done := make(chan struct{})

	p.Register(7)
	go func() {
		_, gotErr = p.Wait(context.Background(), 7)
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	p.Resolve(7, nil, &Error{Code: -32600, Message: "invalid request"})

	<-done

	if gotErr == nil {
		t.Fatal("expected error, got nil")
	}

	want := "rpc error -32600: invalid request"
	if gotErr.Error() != want {
		t.Fatalf("error = %q, want %q", gotErr.Error(), want)
	}
}
