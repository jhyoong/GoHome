package openai

import (
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// defaultRetryBackoff is the delay schedule used by Client.Stream between attempts.
var defaultRetryBackoff = common.DefaultBackoff

// Ensure the package-level alias is the right type.
var _ []time.Duration = defaultRetryBackoff
