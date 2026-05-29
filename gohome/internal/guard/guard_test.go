package guard

import (
	"context"
	"encoding/json"
	"testing"
)

// fakeFrontend is a test double for the Frontend interface.
type fakeFrontend struct {
	called   bool
	lastReq  ApprovalRequest
	response ApprovalDecision
	err      error
}

func (f *fakeFrontend) RequestApproval(_ context.Context, req ApprovalRequest) (ApprovalDecision, error) {
	f.called = true
	f.lastReq = req
	return f.response, f.err
}

func newTestGuard(wl *Whitelist, fe Frontend) *Guard {
	return NewGuard(wl, fe)
}

func emptyWhitelist(t *testing.T) *Whitelist {
	t.Helper()
	wl, err := Compile(WhitelistFile{}, WhitelistFile{}, "")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return wl
}

func whitelistWith(t *testing.T, tools []string, bash []string) *Whitelist {
	t.Helper()
	wl, err := Compile(WhitelistFile{Tools: tools, Bash: bash}, WhitelistFile{}, "")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return wl
}

func bashCmd(cmd string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"command": cmd})
	return b
}

// Task 7.5 tests

func TestCheck_Yolo_NoFrontendCall(t *testing.T) {
	fe := &fakeFrontend{}
	g := newTestGuard(emptyWhitelist(t), fe)
	g.SetYolo(true)

	dec, err := g.Check(context.Background(), "sess1", "bash", bashCmd("rm -rf /"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dec.Allow {
		t.Error("yolo: expected Allow=true")
	}
	if dec.Reason != "yolo" {
		t.Errorf("yolo: expected reason 'yolo', got %q", dec.Reason)
	}
	if fe.called {
		t.Error("yolo: frontend should not be called")
	}
}

func TestCheck_Whitelisted_NoFrontendCall(t *testing.T) {
	fe := &fakeFrontend{}
	wl := whitelistWith(t, []string{"read"}, nil)
	g := newTestGuard(wl, fe)

	dec, err := g.Check(context.Background(), "sess1", "read", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dec.Allow {
		t.Error("whitelisted: expected Allow=true")
	}
	if dec.Reason != "whitelisted" {
		t.Errorf("whitelisted: expected reason 'whitelisted', got %q", dec.Reason)
	}
	if fe.called {
		t.Error("whitelisted: frontend should not be called")
	}
}

func TestCheck_AllowOnce(t *testing.T) {
	fe := &fakeFrontend{
		response: ApprovalDecision{Outcome: AllowOnce},
	}
	g := newTestGuard(emptyWhitelist(t), fe)

	dec, err := g.Check(context.Background(), "sess1", "write", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dec.Allow {
		t.Error("allow_once: expected Allow=true")
	}
	if dec.Reason != "user_once" {
		t.Errorf("allow_once: expected reason 'user_once', got %q", dec.Reason)
	}
	if !fe.called {
		t.Error("allow_once: frontend should have been called")
	}
}

func TestCheck_AllowAlways(t *testing.T) {
	// Use a tmp file so AddProject has a path to write to.
	tmpDir := t.TempDir()
	projPath := tmpDir + "/whitelist.json"

	wl, err := Compile(WhitelistFile{}, WhitelistFile{}, projPath)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	fe := &fakefrontend_allowAlways{
		decision: ApprovalDecision{Outcome: AllowAlways, SavedPattern: "^git status"},
	}
	g := NewGuard(wl, fe)

	dec, err := g.Check(context.Background(), "sess1", "bash", bashCmd("git status"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dec.Allow {
		t.Error("allow_always: expected Allow=true")
	}
	if dec.Reason != "user_always" {
		t.Errorf("allow_always: expected reason 'user_always', got %q", dec.Reason)
	}

	// Pattern should now be persisted; a second call should be whitelisted.
	fe2 := &fakeFrontend{}
	g2 := NewGuard(wl, fe2)
	dec2, err := g2.Check(context.Background(), "sess1", "bash", bashCmd("git status"))
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if !dec2.Allow {
		t.Error("allow_always second call: expected Allow=true")
	}
	if fe2.called {
		t.Error("allow_always second call: frontend should not be called (pattern persisted)")
	}
}

type fakefrontend_allowAlways struct {
	called   bool
	decision ApprovalDecision
}

func (f *fakefrontend_allowAlways) RequestApproval(_ context.Context, req ApprovalRequest) (ApprovalDecision, error) {
	f.called = true
	return f.decision, nil
}

func TestCheck_Deny(t *testing.T) {
	fe := &fakeFrontend{
		response: ApprovalDecision{Outcome: Deny},
	}
	g := newTestGuard(emptyWhitelist(t), fe)

	dec, err := g.Check(context.Background(), "sess1", "bash", bashCmd("rm -rf /"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Allow {
		t.Error("deny: expected Allow=false")
	}
	if dec.Reason != "user_denied" {
		t.Errorf("deny: expected reason 'user_denied', got %q", dec.Reason)
	}
}

func TestCheck_DenySteer(t *testing.T) {
	fe := &fakeFrontend{
		response: ApprovalDecision{
			Outcome:      DenySteer,
			SteerMessage: "please use a safer command",
		},
	}
	g := newTestGuard(emptyWhitelist(t), fe)

	dec, err := g.Check(context.Background(), "sess1", "bash", bashCmd("rm -rf /"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Allow {
		t.Error("deny_steer: expected Allow=false")
	}
	if dec.Reason != "user_denied_steer" {
		t.Errorf("deny_steer: expected reason 'user_denied_steer', got %q", dec.Reason)
	}
	if dec.SteerMessage != "please use a safer command" {
		t.Errorf("deny_steer: expected steer message, got %q", dec.SteerMessage)
	}
}

func TestCheck_ApprovalRequest_Summary(t *testing.T) {
	fe := &fakeFrontend{
		response: ApprovalDecision{Outcome: AllowOnce},
	}
	g := newTestGuard(emptyWhitelist(t), fe)

	// For bash, summary should be the command.
	_, _ = g.Check(context.Background(), "sess1", "bash", bashCmd("git status"))
	if fe.lastReq.Summary != "git status" {
		t.Errorf("bash summary: got %q, want %q", fe.lastReq.Summary, "git status")
	}

	fe2 := &fakeFrontend{
		response: ApprovalDecision{Outcome: AllowOnce},
	}
	g2 := newTestGuard(emptyWhitelist(t), fe2)
	_, _ = g2.Check(context.Background(), "sess1", "write", json.RawMessage(`{}`))
	if fe2.lastReq.Summary != "write" {
		t.Errorf("non-bash summary: got %q, want %q", fe2.lastReq.Summary, "write")
	}
}
