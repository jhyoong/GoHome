package session

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

func TestValidateHistory_ValidThinkingWithSignature(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	msgs := []common.Message{
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockThinking, Text: "reasoning here", Signature: "sig123"},
			{Kind: common.BlockText, Text: "answer"},
		}},
	}

	ValidateHistory(logger, "sess-1", msgs)

	if buf.Len() != 0 {
		t.Errorf("expected no warnings, got: %s", buf.String())
	}
}

func TestValidateHistory_ValidThinkingWithoutSignature(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	msgs := []common.Message{
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockThinking, Text: "openai reasoning"},
			{Kind: common.BlockText, Text: "answer"},
		}},
	}

	ValidateHistory(logger, "sess-1", msgs)

	if buf.Len() != 0 {
		t.Errorf("expected no warnings for empty signature (OpenAI), got: %s", buf.String())
	}
}

func TestValidateHistory_EmptyTextWarns(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	msgs := []common.Message{
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockThinking, Text: ""},
			{Kind: common.BlockText, Text: "answer"},
		}},
	}

	ValidateHistory(logger, "sess-1", msgs)

	if !strings.Contains(buf.String(), "empty thinking block") {
		t.Errorf("expected warning about empty thinking block, got: %s", buf.String())
	}
}

func TestValidateHistory_MixedBlocksOnlyWarnsInvalid(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	msgs := []common.Message{
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockThinking, Text: "valid thinking"},
			{Kind: common.BlockText, Text: "text"},
		}},
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockThinking, Text: ""},
			{Kind: common.BlockText, Text: "more text"},
		}},
	}

	ValidateHistory(logger, "sess-1", msgs)

	output := buf.String()
	if !strings.Contains(output, "empty thinking block") {
		t.Errorf("expected warning for empty block, got: %s", output)
	}
	count := strings.Count(output, "empty thinking block")
	if count != 1 {
		t.Errorf("expected 1 warning, got %d", count)
	}
}

func TestValidateHistory_NoThinkingBlocks(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	msgs := []common.Message{
		{Role: common.RoleUser, Content: []common.Block{
			{Kind: common.BlockText, Text: "hello"},
		}},
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockText, Text: "hi"},
		}},
	}

	ValidateHistory(logger, "sess-1", msgs)

	if buf.Len() != 0 {
		t.Errorf("expected no warnings, got: %s", buf.String())
	}
}
