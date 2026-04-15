package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	defaultHTTPClientTimeout     = 30 * time.Second
	maxHTTPClientTimeout         = 120 * time.Second
	defaultMaxHTTPResponseBytes  = 10 * 1024 * 1024 // 10MB
)

type httpRequestInput struct {
	URL              string            `json:"url"`
	Method           string            `json:"method,omitempty"`
	Headers          map[string]string `json:"headers,omitempty"`
	Body             string            `json:"body,omitempty"`
	TimeoutMs        int               `json:"timeoutMs,omitempty"`
	MaxResponseBytes int               `json:"maxResponseBytes,omitempty"`
	FollowRedirects  *bool             `json:"followRedirects,omitempty"`
}

type httpResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	URL     string            `json:"url,omitempty"`
}

func handleHttpRequest(logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)

		var input httpRequestInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}

		if input.URL == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "url is required"})
			return
		}

		if !strings.HasPrefix(input.URL, "http://") && !strings.HasPrefix(input.URL, "https://") {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "url must use http or https scheme"})
			return
		}

		method := input.Method
		if method == "" {
			method = http.MethodGet
		}

		timeout := defaultHTTPClientTimeout
		if input.TimeoutMs > 0 {
			timeout = time.Duration(input.TimeoutMs) * time.Millisecond
			if timeout > maxHTTPClientTimeout {
				timeout = maxHTTPClientTimeout
			}
		}

		maxBytes := defaultMaxHTTPResponseBytes
		if input.MaxResponseBytes > 0 && input.MaxResponseBytes < defaultMaxHTTPResponseBytes {
			maxBytes = input.MaxResponseBytes
		}

		followRedirects := true
		if input.FollowRedirects != nil {
			followRedirects = *input.FollowRedirects
		}

		var bodyReader io.Reader
		if input.Body != "" {
			bodyReader = strings.NewReader(input.Body)
		}

		req, err := http.NewRequestWithContext(r.Context(), method, input.URL, bodyReader)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: fmt.Sprintf("invalid request: %v", err)})
			return
		}

		for k, v := range input.Headers {
			req.Header.Set(k, v)
		}

		client := &http.Client{Timeout: timeout}
		if !followRedirects {
			client.CheckRedirect = func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			}
		}

		logger.Info("http/request", "method", method, "url", input.URL)

		resp, err := client.Do(req)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, ErrorResponse{
				Error:  "request failed",
				Detail: err.Error(),
			})
			return
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, ErrorResponse{
				Error:  "failed to read response",
				Detail: err.Error(),
			})
			return
		}
		if len(respBody) > maxBytes {
			writeJSON(w, http.StatusBadGateway, ErrorResponse{
				Error: fmt.Sprintf("response exceeds %d byte limit", maxBytes),
			})
			return
		}

		respHeaders := make(map[string]string)
		for k := range resp.Header {
			respHeaders[strings.ToLower(k)] = resp.Header.Get(k)
		}

		finalURL := ""
		if resp.Request != nil && resp.Request.URL.String() != input.URL {
			finalURL = resp.Request.URL.String()
		}

		writeJSON(w, http.StatusOK, httpResponse{
			Status:  resp.StatusCode,
			Headers: respHeaders,
			Body:    string(respBody),
			URL:     finalURL,
		})
	}
}
