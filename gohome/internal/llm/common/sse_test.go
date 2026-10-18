package common

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"testing/iotest"
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

func TestParseSSE_CRLFSplitAcrossReads(t *testing.T) {
	in := "event: a\r\ndata: 1\r\n\r\nevent: b\r\ndata: 2\r\n\r\n"
	want := collect(t, in)
	var got []SSEFrame
	for f := range ParseSSE(context.Background(), iotest.OneByteReader(strings.NewReader(in))) {
		got = append(got, f)
	}
	if len(want) != 2 || len(got) != len(want) {
		t.Fatalf("got %d frames, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("frame %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseSSE_BlankLinesAndComments(t *testing.T) {
	got := collect(t, ": comment\n\n\n\ndata: {\"choices\":[]}\n\ndata: [DONE]\n\n")
	if len(got) != 2 || got[0].Data != `{"choices":[]}` || got[1].Data != "[DONE]" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseSSE_ReadError(t *testing.T) {
	boom := errors.New("boom")
	r := io.MultiReader(strings.NewReader("data: partial"), iotest.ErrReader(boom))
	got := collect2(r)
	if len(got) != 1 || !errors.Is(got[0].Err, boom) {
		t.Fatalf("got %+v", got)
	}
}

func collect2(r io.Reader) []SSEFrame {
	var out []SSEFrame
	for f := range ParseSSE(context.Background(), r) {
		out = append(out, f)
	}
	return out
}

func TestParseSSE_AnthropicFixture(t *testing.T) {
	f, err := os.Open("../anthropic/testdata/simple_text.sse")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = f.Close() }()
	wantEvents := []string{"message_start", "content_block_start", "content_block_delta", "content_block_delta", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}
	got := collect2(f)
	if len(got) != len(wantEvents) {
		t.Fatalf("got %d frames, want %d", len(got), len(wantEvents))
	}
	for i, fr := range got {
		if fr.Event != wantEvents[i] || fr.Data == "" || fr.Err != nil {
			t.Errorf("frame %d: %+v, want event %q with data", i, fr, wantEvents[i])
		}
	}
	if !strings.Contains(got[2].Data, `"text":"Hello"`) {
		t.Errorf("frame 2 data: %q", got[2].Data)
	}
}

func TestParseSSE_OpenAIFixture(t *testing.T) {
	f, err := os.Open("../openai/testdata/simple_text.sse")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = f.Close() }()
	got := collect2(f)
	// 6 data chunks plus the [DONE] sentinel.
	if len(got) != 7 {
		t.Fatalf("got %d frames, want 7", len(got))
	}
	if got[6].Data != "[DONE]" {
		t.Errorf("last frame: %q", got[6].Data)
	}
	for i, fr := range got {
		if fr.Event != "" || fr.Data == "" {
			t.Errorf("frame %d: %+v", i, fr)
		}
	}
}
