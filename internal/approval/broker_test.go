package approval_test

import (
	"context"
	"testing"
	"time"

	"github.com/JiaHui/gohome/internal/approval"
	"github.com/JiaHui/gohome/internal/config"
)

func TestAutoApproveWhitelist(t *testing.T) {
	cfg := config.ApprovalConfig{
		DefaultTimeout: 5,
		Whitelist:      []config.WhitelistEntry{{Tool: "file_read", Allow: "always"}},
	}
	broker := approval.NewBroker(cfg, nil)
	approved, err := broker.Request(context.Background(), "r1", "file_read", []byte(`{}`))
	if err != nil || !approved {
		t.Errorf("expected auto-approve; got approved=%v err=%v", approved, err)
	}
}

func TestAutoDenyWhitelist(t *testing.T) {
	cfg := config.ApprovalConfig{
		DefaultTimeout: 5,
		Whitelist:      []config.WhitelistEntry{{Tool: "shell", Allow: "never"}},
	}
	broker := approval.NewBroker(cfg, nil)
	approved, err := broker.Request(context.Background(), "r2", "shell", []byte(`{}`))
	if err != nil || approved {
		t.Errorf("expected auto-deny; got approved=%v err=%v", approved, err)
	}
}

func TestAutoApproveAll(t *testing.T) {
	cfg := config.ApprovalConfig{DefaultTimeout: 5, AutoApproveAll: true}
	broker := approval.NewBroker(cfg, nil)
	approved, err := broker.Request(context.Background(), "r3", "anything", []byte(`{}`))
	if err != nil || !approved {
		t.Errorf("expected auto-approve-all; got approved=%v err=%v", approved, err)
	}
}

func TestApprovalTimeout(t *testing.T) {
	cfg := config.ApprovalConfig{DefaultTimeout: 1} // 1 second
	send := make(chan approval.Request, 1)
	broker := approval.NewBroker(cfg, send)
	approved, err := broker.Request(context.Background(), "r4", "unknown", []byte(`{}`))
	if err == nil {
		t.Error("expected timeout error")
	}
	if approved {
		t.Error("expected false on timeout")
	}
}

func TestApprovalContextCancel(t *testing.T) {
	cfg := config.ApprovalConfig{DefaultTimeout: 60}
	send := make(chan approval.Request, 1)
	broker := approval.NewBroker(cfg, send)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	approved, err := broker.Request(ctx, "r5", "unknown", []byte(`{}`))
	if err == nil {
		t.Error("expected context error")
	}
	if approved {
		t.Error("expected false on cancel")
	}
}

func TestApprovalUserDecision(t *testing.T) {
	cfg := config.ApprovalConfig{DefaultTimeout: 5}
	send := make(chan approval.Request, 1)
	broker := approval.NewBroker(cfg, send)
	go func() {
		req := <-send
		broker.Respond(req.ID, true)
	}()
	approved, err := broker.Request(context.Background(), "r6", "unknown", []byte(`{}`))
	if err != nil || !approved {
		t.Errorf("expected user approval; got approved=%v err=%v", approved, err)
	}
}

func TestShellAutoApproveWithPattern(t *testing.T) {
	cfg := config.ApprovalConfig{
		Whitelist: []config.WhitelistEntry{
			{Tool: "shell", Allow: "always", CommandPattern: "ls *"},
		},
	}
	broker := approval.NewBroker(cfg, nil)
	approved, err := broker.Request(context.Background(), "r10", "shell", []byte(`{"command":"ls -la /tmp"}`))
	if err != nil || !approved {
		t.Errorf("expected auto-approve; got approved=%v err=%v", approved, err)
	}
}

func TestShellPatternNoMatchFallsThrough(t *testing.T) {
	cfg := config.ApprovalConfig{
		DefaultTimeout: 1,
		Whitelist: []config.WhitelistEntry{
			{Tool: "shell", Allow: "always", CommandPattern: "ls *"},
		},
	}
	send := make(chan approval.Request, 1)
	broker := approval.NewBroker(cfg, send)
	// "cat" does not match "ls *" — should reach approval (timeout)
	_, err := broker.Request(context.Background(), "r11", "shell", []byte(`{"command":"cat /etc/passwd"}`))
	if err == nil {
		t.Error("expected timeout (approval required); got nil error")
	}
}

func TestShellChainedAlwaysPatternFallsThrough(t *testing.T) {
	cfg := config.ApprovalConfig{
		DefaultTimeout: 1,
		Whitelist: []config.WhitelistEntry{
			{Tool: "shell", Allow: "always", CommandPattern: "ls *"},
		},
	}
	send := make(chan approval.Request, 1)
	broker := approval.NewBroker(cfg, send)
	// Chained command matching the pattern must NOT be auto-approved
	_, err := broker.Request(context.Background(), "r12", "shell", []byte(`{"command":"ls /tmp && rm -rf /"}`))
	if err == nil {
		t.Error("expected timeout (chained command must reach approval); got nil error")
	}
}

func TestShellNeverWithPattern(t *testing.T) {
	cfg := config.ApprovalConfig{
		Whitelist: []config.WhitelistEntry{
			{Tool: "shell", Allow: "never", CommandPattern: "rm *"},
		},
	}
	broker := approval.NewBroker(cfg, nil)
	approved, err := broker.Request(context.Background(), "r13", "shell", []byte(`{"command":"rm -rf /tmp/foo"}`))
	if err != nil || approved {
		t.Errorf("expected auto-deny; got approved=%v err=%v", approved, err)
	}
}

func TestShellNeverChainedStillDenies(t *testing.T) {
	cfg := config.ApprovalConfig{
		Whitelist: []config.WhitelistEntry{
			{Tool: "shell", Allow: "never", CommandPattern: "rm *"},
		},
	}
	broker := approval.NewBroker(cfg, nil)
	// "never" entries apply even to chained commands
	approved, err := broker.Request(context.Background(), "r14", "shell", []byte(`{"command":"rm -rf / && echo done"}`))
	if err != nil || approved {
		t.Errorf("expected auto-deny for chained never; got approved=%v err=%v", approved, err)
	}
}

func TestAddWhitelistEntryRuntimeUpdate(t *testing.T) {
	cfg := config.ApprovalConfig{DefaultTimeout: 1}
	broker := approval.NewBroker(cfg, nil)

	// Before adding: should timeout
	_, err := broker.Request(context.Background(), "r15", "file_read", []byte(`{}`))
	if err == nil {
		t.Error("expected timeout before adding entry")
	}

	broker.AddWhitelistEntry(config.WhitelistEntry{Tool: "file_read", Allow: "always"})

	// After adding: should auto-approve
	approved, err := broker.Request(context.Background(), "r16", "file_read", []byte(`{}`))
	if err != nil || !approved {
		t.Errorf("expected auto-approve after AddWhitelistEntry; got approved=%v err=%v", approved, err)
	}
}
