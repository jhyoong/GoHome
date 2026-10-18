package anthropic

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/iotest"
)

func TestParseSSE_MapsEventAndData(t *testing.T) {
	in := "event: message_stop\r\ndata: {\"type\":\"message_stop\"}\r\n\r\n"
	var got []sseFrame
	for f := range parseSSE(context.Background(), strings.NewReader(in)) {
		got = append(got, f)
	}
	if len(got) != 1 || got[0].event != "message_stop" || got[0].data != `{"type":"message_stop"}` {
		t.Fatalf("got %+v", got)
	}
}

func TestParseSSE_ReadErrorBecomesErrorFrame(t *testing.T) {
	var got []sseFrame
	for f := range parseSSE(context.Background(), iotest.ErrReader(errors.New("boom"))) {
		got = append(got, f)
	}
	if len(got) != 1 || got[0].event != "error" || !strings.Contains(got[0].data, "boom") {
		t.Fatalf("got %+v", got)
	}
}
