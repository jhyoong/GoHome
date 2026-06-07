package session

import (
	"log/slog"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// ValidateHistory checks loaded session messages for malformed thinking blocks.
// It logs warnings but does not modify or remove any messages.
func ValidateHistory(logger *slog.Logger, sessionID string, msgs []common.Message) {
	for i, msg := range msgs {
		for j, b := range msg.Content {
			if b.Kind != common.BlockThinking {
				continue
			}
			if b.Text == "" {
				logger.Warn("empty thinking block",
					"session", sessionID,
					"message", i,
					"block", j,
				)
			}
		}
	}
}
