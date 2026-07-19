package headless

import (
	"bytes"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
)

var _ agent.Frontend = (*Frontend)(nil)

func TestNewFrontend(t *testing.T) {
	var buf bytes.Buffer
	fe := NewFrontend("hello", false, &buf)
	if fe == nil {
		t.Fatal("NewFrontend returned nil")
	}
}
