package cmd

import (
	"fmt"
	"net/http"
)

// deriveBaseURL returns the base URL for the server from the request context.
func deriveBaseURL(r *http.Request, port int) string {
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	if host := r.Host; host != "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		baseURL = fmt.Sprintf("%s://%s", scheme, host)
	}
	return baseURL
}
