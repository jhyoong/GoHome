package openai

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/iotest"
)

func collectFrames(r *strings.Reader) []sseFrame {
	var out []sseFrame
	for f := range parseSSE(context.Background(), r) {
		out = append(out, f)
	}
	return out
}

func TestParseSSE_CRLFAndDone(t *testing.T) {
	frames := collectFrames(strings.NewReader("data: {\"choices\":[]}\r\n\r\ndata: [DONE]\r\n\r\ndata: after\r\n\r\n"))
	if len(frames) != 2 || frames[0].data != `{"choices":[]}` || !frames[1].done {
		t.Fatalf("unexpected frames: %+v", frames)
	}
}

func TestParseSSE_SkipsEventsWithoutData(t *testing.T) {
	frames := collectFrames(strings.NewReader("event: ping\n\ndata: x\n\n"))
	if len(frames) != 1 || frames[0].data != "x" {
		t.Fatalf("unexpected frames: %+v", frames)
	}
}

func TestParseSSE_ReadErrorFrame(t *testing.T) {
	var got []sseFrame
	for f := range parseSSE(context.Background(), iotest.ErrReader(errors.New("boom"))) {
		got = append(got, f)
	}
	if len(got) != 1 || got[0].err == nil {
		t.Fatalf("got %+v", got)
	}
}
