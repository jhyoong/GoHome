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
