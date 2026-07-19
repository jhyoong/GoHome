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
	return NewGuard(wl, fe, nil)
}

func emptyWhitelist(t *testing.T) *Whitelist {
	t.Helper()
	wl, err := Compile(WhitelistFile{}, WhitelistFile{}, "")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return wl
}

func whitelistWith(t *testing.T, tools []string, shell []string) *Whitelist {
	t.Helper()
	wl, err := Compile(WhitelistFile{Tools: tools, Shell: shell}, WhitelistFile{}, "")
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

	dec, err := g.Check(context.Background(), "sess1", "shell", bashCmd("rm -rf /"))
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
	g := NewGuard(wl, fe, nil)

	dec, err := g.Check(context.Background(), "sess1", "shell", bashCmd("git status"))
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
	g2 := NewGuard(wl, fe2, nil)
	dec2, err := g2.Check(context.Background(), "sess1", "shell", bashCmd("git status"))
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

func TestCheck_AllowAlways_SavedPattern(t *testing.T) {
	tmpDir := t.TempDir()
	projPath := tmpDir + "/whitelist.json"

	wl, err := Compile(WhitelistFile{}, WhitelistFile{}, projPath)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	const wantPattern = "^git log"
	fe := &fakefrontend_allowAlways{
		decision: ApprovalDecision{Outcome: AllowAlways, SavedPattern: wantPattern},
	}
	g := NewGuard(wl, fe, nil)

	dec, err := g.Check(context.Background(), "sess1", "shell", bashCmd("git log"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dec.Allow {
		t.Error("allow_always: expected Allow=true")
	}
	if dec.Reason != "user_always" {
		t.Errorf("allow_always: expected reason 'user_always', got %q", dec.Reason)
	}
	if dec.SavedPattern != wantPattern {
		t.Errorf("allow_always: Decision.SavedPattern = %q, want %q", dec.SavedPattern, wantPattern)
	}
}

func TestCheck_Deny(t *testing.T) {
	fe := &fakeFrontend{
		response: ApprovalDecision{Outcome: Deny},
	}
	g := newTestGuard(emptyWhitelist(t), fe)

	dec, err := g.Check(context.Background(), "sess1", "shell", bashCmd("rm -rf /"))
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

	dec, err := g.Check(context.Background(), "sess1", "shell", bashCmd("rm -rf /"))
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

func TestCheck_SudoCommand_SetsNeedsSudoPassword(t *testing.T) {
	fe := &fakeFrontend{
		response: ApprovalDecision{Outcome: AllowOnce},
	}
	g := newTestGuard(emptyWhitelist(t), fe)

	_, _ = g.Check(context.Background(), "sess1", "shell", bashCmd("sudo apt install vim"))
	if !fe.lastReq.NeedsSudoPassword {
		t.Error("expected NeedsSudoPassword=true for sudo command")
	}
}

func TestCheck_NonSudoCommand_DoesNotSetNeedsSudoPassword(t *testing.T) {
	fe := &fakeFrontend{
		response: ApprovalDecision{Outcome: AllowOnce},
	}
	g := newTestGuard(emptyWhitelist(t), fe)

	_, _ = g.Check(context.Background(), "sess1", "shell", bashCmd("ls -la"))
	if fe.lastReq.NeedsSudoPassword {
		t.Error("expected NeedsSudoPassword=false for non-sudo command")
	}
}

func TestCheck_SudoPassword_ThreadedToDecision(t *testing.T) {
	fe := &fakeFrontend{
		response: ApprovalDecision{Outcome: AllowOnce, SudoPassword: "secret123"},
	}
	g := newTestGuard(emptyWhitelist(t), fe)

	dec, err := g.Check(context.Background(), "sess1", "shell", bashCmd("sudo apt install vim"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.SudoPassword != "secret123" {
		t.Errorf("SudoPassword = %q, want %q", dec.SudoPassword, "secret123")
	}
}

func TestDecision_DenyInfo_Field(t *testing.T) {
	d := Decision{
		Allow:    false,
		Reason:   "denylisted",
		DenyInfo: "command denied by denylist: matched pattern 'rm -rf /'",
	}
	if d.DenyInfo == "" {
		t.Error("expected DenyInfo to be set")
	}
}

func TestCheck_ApprovalRequest_Summary(t *testing.T) {
	fe := &fakeFrontend{
		response: ApprovalDecision{Outcome: AllowOnce},
	}
	g := newTestGuard(emptyWhitelist(t), fe)

	// For shell, summary should be the command.
	_, _ = g.Check(context.Background(), "sess1", "shell", bashCmd("git status"))
	if fe.lastReq.Summary != "git status" {
		t.Errorf("shell summary: got %q, want %q", fe.lastReq.Summary, "git status")
	}

	fe2 := &fakeFrontend{
		response: ApprovalDecision{Outcome: AllowOnce},
	}
	g2 := newTestGuard(emptyWhitelist(t), fe2)
	_, _ = g2.Check(context.Background(), "sess1", "write", json.RawMessage(`{}`))
	if fe2.lastReq.Summary != "write" {
		t.Errorf("non-shell summary: got %q, want %q", fe2.lastReq.Summary, "write")
	}
}

func TestCheck_Denylist_BlocksBeforeYolo(t *testing.T) {
	fe := &fakeFrontend{}
	dl, _ := CompileDenylist(DenylistFile{Shell: []string{"rm -rf /"}})
	wl := emptyWhitelist(t)
	g := NewGuard(wl, fe, dl)
	g.SetYolo(true)

	dec, err := g.Check(context.Background(), "sess1", "shell", bashCmd("rm -rf /"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Allow {
		t.Error("denylist: expected Allow=false even in yolo mode")
	}
	if dec.Reason != "denylisted" {
		t.Errorf("denylist: expected reason 'denylisted', got %q", dec.Reason)
	}
	if dec.DenyInfo == "" {
		t.Error("denylist: expected DenyInfo to be set")
	}
	if fe.called {
		t.Error("denylist: frontend should not be called")
	}
}

func TestCheck_Denylist_BlocksBeforeWhitelist(t *testing.T) {
	fe := &fakeFrontend{}
	dl, _ := CompileDenylist(DenylistFile{Shell: []string{"rm -rf /"}})
	wl := whitelistWith(t, nil, []string{"^rm"})
	g := NewGuard(wl, fe, dl)

	dec, err := g.Check(context.Background(), "sess1", "shell", bashCmd("rm -rf /"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Allow {
		t.Error("denylist: expected Allow=false even when whitelisted")
	}
	if dec.Reason != "denylisted" {
		t.Errorf("denylist: expected reason 'denylisted', got %q", dec.Reason)
	}
}

func TestCheck_Denylist_NonMatchingCommand_PassesThrough(t *testing.T) {
	fe := &fakeFrontend{response: ApprovalDecision{Outcome: AllowOnce}}
	dl, _ := CompileDenylist(DenylistFile{Shell: []string{"rm -rf /"}})
	wl := emptyWhitelist(t)
	g := NewGuard(wl, fe, dl)

	dec, err := g.Check(context.Background(), "sess1", "shell", bashCmd("ls -la"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dec.Allow {
		t.Error("expected non-matching command to pass through")
	}
}

func TestCheck_Denylist_NonShellTool_NotChecked(t *testing.T) {
	fe := &fakeFrontend{response: ApprovalDecision{Outcome: AllowOnce}}
	dl, _ := CompileDenylist(DenylistFile{Shell: []string{"rm -rf /"}})
	wl := emptyWhitelist(t)
	g := NewGuard(wl, fe, dl)

	dec, err := g.Check(context.Background(), "sess1", "write", json.RawMessage(`{"path":"rm -rf /"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dec.Allow {
		t.Error("expected non-shell tool to pass through denylist")
	}
}

func TestCheck_Denylist_Nil_NoEffect(t *testing.T) {
	fe := &fakeFrontend{response: ApprovalDecision{Outcome: AllowOnce}}
	wl := emptyWhitelist(t)
	g := NewGuard(wl, fe, nil)

	dec, err := g.Check(context.Background(), "sess1", "shell", bashCmd("rm -rf /"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dec.Allow {
		t.Error("expected nil denylist to have no effect")
	}
}
