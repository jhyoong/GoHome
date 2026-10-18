package common

import (
	"context"
	"strings"
	"testing"
)

func collect(t *testing.T, in string) []SSEFrame {
	t.Helper()
	var out []SSEFrame
	for f := range ParseSSE(context.Background(), strings.NewReader(in)) {
		out = append(out, f)
	}
	return out
}

func TestParseSSE_LineEndings(t *testing.T) {
	want := []SSEFrame{{Event: "a", Data: "1"}, {Event: "b", Data: "2"}}
	for name, in := range map[string]string{
		"LF":    "event: a\ndata: 1\n\nevent: b\ndata: 2\n\n",
		"CRLF":  "event: a\r\ndata: 1\r\n\r\nevent: b\r\ndata: 2\r\n\r\n",
		"CR":    "event: a\rdata: 1\r\revent: b\rdata: 2\r\r",
		"mixed": "event: a\r\ndata: 1\n\revent: b\ndata: 2\r\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			got := collect(t, in)
			if len(got) != len(want) {
				t.Fatalf("got %d frames, want %d: %+v", len(got), len(want), got)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("frame %d: got %+v, want %+v", i, got[i], want[i])
				}
			}
		})
	}
}

func TestParseSSE_MultiLineData(t *testing.T) {
	got := collect(t, "data: line1\ndata: line2\n\n")
	if len(got) != 1 || got[0].Data != "line1\nline2" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseSSE_CommentsAndTrailingEvent(t *testing.T) {
	got := collect(t, ": hi\n\ndata: x\n\ndata: tail")
	if len(got) != 2 || got[0].Data != "x" || got[1].Data != "tail" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseSSE_CtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := ParseSSE(ctx, strings.NewReader("data: a\n\ndata: b\n\n"))
	<-ch
	cancel()
	for range ch {
	}
}
