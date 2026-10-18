package tui

import (
	"strings"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// historyToTimeline rebuilds timeline entries from saved session history.
// Saved text is untrusted like live agent output, so it is sanitized.
func historyToTimeline(msgs []common.Message) []TimelineEntry {
	var entries []TimelineEntry
	for _, msg := range msgs {
		switch msg.Role {
		case common.RoleUser:
			var parts []string
			for _, b := range msg.Content {
				if b.Kind == common.BlockText && b.Text != "" {
					parts = append(parts, sanitizeText(b.Text))
				}
			}
			if len(parts) > 0 {
				entries = append(entries, TimelineEntry{
					Kind: KindUser,
					Text: strings.Join(parts, "\n"),
				})
			}

		case common.RoleAssistant:
			for _, b := range msg.Content {
				switch b.Kind {
				case common.BlockThinking:
					entries = append(entries, TimelineEntry{
						Kind:     KindThinking,
						Text:     sanitizeText(b.Text),
						Expanded: true, // fully shown by default; Enter collapses
					})
				case common.BlockText:
					entries = append(entries, TimelineEntry{
						Kind: KindAssistant,
						Text: sanitizeText(b.Text),
					})
				case common.BlockToolUse:
					entries = append(entries, TimelineEntry{
						Kind:      KindTool,
						ToolName:  sanitizeText(b.ToolName),
						ToolUseID: b.ToolUseID,
						Text:      sanitizeText(b.InputJSON),
						Status:    "success",
					})
				}
			}
			if notice := common.TokenLimitNotice(msg.StopReason); notice != "" {
				entries = append(entries, TimelineEntry{Kind: KindNotice, Text: notice})
			}

		case common.RoleTool:
			for _, b := range msg.Content {
				if b.Kind != common.BlockToolResult {
					continue
				}
				status := "success"
				if b.IsError {
					status = "error"
				}
				merged := false
				for i := len(entries) - 1; i >= 0; i-- {
					e := &entries[i]
					if e.Kind != KindTool || e.ToolResult != "" {
						continue
					}
					if b.ToolUseID != "" && e.ToolUseID != "" && e.ToolUseID != b.ToolUseID {
						continue
					}
					e.ToolResult = sanitizeText(b.ResultText)
					e.Status = status
					merged = true
					break
				}
				if !merged {
					entries = append(entries, TimelineEntry{
						Kind:       KindTool,
						ToolUseID:  b.ToolUseID,
						ToolResult: sanitizeText(b.ResultText),
						Status:     status,
					})
				}
			}
		}
	}
	return entries
}
