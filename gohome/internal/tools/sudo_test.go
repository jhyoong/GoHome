package tools

import (
	"context"
	"testing"
)

func TestSudoPasswordFromContext(t *testing.T) {
	ctx := WithSudoPassword(context.Background(), "mypassword")
	got := SudoPasswordFrom(ctx)
	if got != "mypassword" {
		t.Errorf("SudoPasswordFrom = %q, want %q", got, "mypassword")
	}
}

func TestSudoPasswordFromContext_Empty(t *testing.T) {
	got := SudoPasswordFrom(context.Background())
	if got != "" {
		t.Errorf("SudoPasswordFrom on bare context = %q, want empty", got)
	}
}
