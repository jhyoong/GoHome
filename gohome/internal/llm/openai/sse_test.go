package openai

import (
	"bytes"
	"context"
	"os"
	"testing"
)

func TestParseSSE_Fixture(t *testing.T) {
	f, err := os.Open("testdata/simple_text.sse")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	frames := parseSSE(context.Background(), f)

	// The fixture has:
	//  1. role delta chunk (content="")
	//  2. "Hello" chunk
	//  3. ", world" chunk
	//  4. "!" chunk
	//  5. finish_reason:"stop" chunk (delta={})
	//  6. usage chunk (choices:[])
	//  7. [DONE] sentinel
	//
	// We expect 6 regular data frames and 1 [DONE] frame.
	var dataFrames []sseFrame
	var doneCount int
	for fr := range frames {
		if fr.done {
			doneCount++
		} else {
			dataFrames = append(dataFrames, fr)
		}
	}

	if doneCount != 1 {
		t.Errorf("expected 1 [DONE] frame, got %d", doneCount)
	}
	if len(dataFrames) != 6 {
		t.Errorf("expected 6 data frames, got %d", len(dataFrames))
		for i, df := range dataFrames {
			t.Logf("  frame %d: done=%v data=%q", i, df.done, df.data)
		}
	}
}

func TestParseSSE_BlankAndComment(t *testing.T) {
	// Comments and extra blanks must not produce extra frames.
	input := ": this is a comment\n\ndata: {\"choices\":[]}\n\ndata: [DONE]\n\n"
	r := bytes.NewReader([]byte(input))
	ch := parseSSE(context.Background(), r)

	var frames []sseFrame
	for f := range ch {
		frames = append(frames, f)
	}

	// expect 1 data frame + 1 done frame
	if len(frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(frames))
	}
	if frames[0].done {
		t.Error("frame 0 should not be [DONE]")
	}
	if !frames[1].done {
		t.Error("frame 1 should be [DONE]")
	}
}

func TestParseSSE_CtxCancel(t *testing.T) {
	// A stream with no [DONE]; cancel context and verify channel closes.
	input := "data: {\"choices\":[]}\n\n"
	ctx, cancel := context.WithCancel(context.Background())
	r := bytes.NewReader([]byte(input))
	ch := parseSSE(ctx, r)

	// Drain first frame then cancel.
	<-ch
	cancel()
	// Drain until close (must not hang).
	for range ch {
	}
}
