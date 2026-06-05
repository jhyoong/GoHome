package anthropic

import (
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// defaultRetryBackoff is the delay schedule used by Client.Stream between attempts.
// It delegates to common.DefaultBackoff so both adapters share the same defaults.
var defaultRetryBackoff = common.DefaultBackoff

// Ensure the package-level alias is the right type.
var _ []time.Duration = defaultRetryBackoff
