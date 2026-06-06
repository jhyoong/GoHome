package tui

import (
	"strings"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

func historyToTimeline(msgs []common.Message) []TimelineEntry {
	var entries []TimelineEntry
	for _, msg := range msgs {
		switch msg.Role {
		case common.RoleUser:
			var parts []string
			for _, b := range msg.Content {
				if b.Kind == common.BlockText && b.Text != "" {
					parts = append(parts, b.Text)
				}
			}
			if len(parts) > 0 {
				entries = append(entries, TimelineEntry{
					Kind: "user",
					Text: strings.Join(parts, "\n"),
				})
			}

		case common.RoleAssistant:
			for _, b := range msg.Content {
				switch b.Kind {
				case common.BlockThinking:
					entries = append(entries, TimelineEntry{
						Kind: "thinking",
						Text: b.Text,
					})
				case common.BlockText:
					entries = append(entries, TimelineEntry{
						Kind: "assistant",
						Text: b.Text,
					})
				case common.BlockToolUse:
					entries = append(entries, TimelineEntry{
						Kind:     "tool",
						ToolName: b.ToolName,
						Text:     b.InputJSON,
						Status:   "success",
					})
				}
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
					if entries[i].Kind == "tool" && entries[i].ToolResult == "" {
						entries[i].ToolResult = b.ResultText
						entries[i].Status = status
						merged = true
						break
					}
				}
				if !merged {
					entries = append(entries, TimelineEntry{
						Kind:       "tool",
						ToolResult: b.ResultText,
						Status:     status,
					})
				}
			}
		}
	}
	return entries
}
