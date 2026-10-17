package common

import (
	"net/http"
	"time"
)

// Default timeouts for LLM HTTP requests. There is no total request timeout,
// because a long streamed answer can legitimately run for many minutes.
const (
	// DefaultResponseHeaderTimeout bounds the wait for response headers
	// after the request is sent.
	DefaultResponseHeaderTimeout = 5 * time.Minute
	// DefaultIdleTimeout bounds the gap between bytes on a streamed body.
	DefaultIdleTimeout = 5 * time.Minute
)

// NewHTTPClient returns an HTTP client whose transport waits at most
// headerTimeout for response headers. Client.Timeout is left at zero so long
// streams are not cut off; StreamRequest applies an idle-read watchdog instead.
func NewHTTPClient(headerTimeout time.Duration) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = headerTimeout
	return &http.Client{Transport: tr}
}
