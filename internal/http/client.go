package http

import (
	"net/http"
	"time"
)

// DefaultClient is the shared HTTP client used for outbound Registry and
// OAuth requests. It is tuned with a 5-second timeout and a higher
// per-host idle connection limit than http.DefaultClient.
var DefaultClient = &http.Client{
	Timeout:   5 * time.Second,
	Transport: newDefaultTransport(),
}

func newDefaultTransport() http.RoundTripper {
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return http.DefaultTransport
	}

	t = t.Clone()
	t.MaxIdleConnsPerHost = 5

	return t
}
