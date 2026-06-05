package session

import "github.com/jhyoong/GoHome/gohome/internal/tools"

// Compile-time check that *Session satisfies tools.SessionState.
var _ tools.SessionState = (*Session)(nil)
