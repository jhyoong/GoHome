package agent

import "errors"

var ErrToolDenied = errors.New("tool call denied by user")

// ErrNoClient is returned when a turn is attempted before any model is configured.
var ErrNoClient = errors.New("no model configured: run /config, then restart gohome")

// ErrStreamEndedEarly is returned when the LLM stream closes before the turn
// is reported as done.
var ErrStreamEndedEarly = errors.New("stream ended early")
