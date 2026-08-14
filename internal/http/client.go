package http

import (
	"net/http"
	"time"
)

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
