package guard

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// captureDebugLog routes the default slog logger to a buffer at debug level
// for the duration of the test.
func captureDebugLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func TestCheck_LogsDecisions(t *testing.T) {
	dl, _ := CompileDenylist(DenylistFile{Shell: []string{"rm -rf /"}})
	cases := []struct {
		name   string
		wl     []string
		resp   ApprovalDecision
		cmd    string
		reason string
	}{
		{"whitelisted", []string{"^ls"}, ApprovalDecision{}, "ls -la", "whitelisted"},
		{"denylisted", nil, ApprovalDecision{}, "rm -rf /", "denylisted"},
		{"prompted", nil, ApprovalDecision{Outcome: Deny}, "make", "user_denied"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureDebugLog(t)
			fe := &fakeFrontend{response: tc.resp}
			g := NewGuard(whitelistWith(t, nil, tc.wl), fe, dl)
			if _, err := g.Check(context.Background(), "sess1", "shell", bashCmd(tc.cmd)); err != nil {
				t.Fatalf("Check: %v", err)
			}
			out := buf.String()
			if !strings.Contains(out, `"msg":"guard decision"`) || !strings.Contains(out, `"reason":"`+tc.reason+`"`) {
				t.Errorf("log missing decision with reason %q:\n%s", tc.reason, out)
			}
			if tc.name == "prompted" && !strings.Contains(out, `"msg":"guard prompt"`) {
				t.Errorf("log missing prompt line:\n%s", out)
			}
		})
	}
}

func TestCheck_LogOmitsSudoPassword(t *testing.T) {
	withGOOS(t, "linux")
	buf := captureDebugLog(t)
	fe := &fakeFrontend{response: ApprovalDecision{Outcome: AllowOnce, SudoPassword: "secret123"}}
	g := newTestGuard(emptyWhitelist(t), fe)
	if _, err := g.Check(context.Background(), "sess1", "shell", bashCmd("sudo apt install vim")); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !strings.Contains(buf.String(), "guard decision") {
		t.Fatalf("no decision logged:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "secret123") {
		t.Errorf("log contains the sudo password:\n%s", buf.String())
	}
}
