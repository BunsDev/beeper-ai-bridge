package utils

import (
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

type AIServicesLoggingTransport struct {
	Base http.RoundTripper
}

func WithAIServicesLogging(client *http.Client) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	clone := *client
	base := clone.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	if _, ok := base.(*AIServicesLoggingTransport); !ok {
		clone.Transport = &AIServicesLoggingTransport{Base: base}
	}
	return &clone
}

func (transport *AIServicesLoggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := transport.Base
	if base == nil {
		base = http.DefaultTransport
	}
	if req == nil || !IsAIServicesRequest(req) {
		return base.RoundTrip(req)
	}

	logAIServicesRequest(req)
	started := time.Now()
	resp, err := base.RoundTrip(req)
	duration := time.Since(started)
	if err != nil {
		logAIServicesResponse(req, resp, duration, err)
		return resp, err
	}
	logAIServicesResponse(req, resp, duration, nil)
	return resp, nil
}

func IsAIServicesRequest(req *http.Request) bool {
	if req == nil || req.URL == nil {
		return false
	}
	host := strings.ToLower(req.URL.Hostname())
	path := strings.ToLower(req.URL.EscapedPath())
	return strings.Contains(host, "ai-services.") ||
		strings.Contains(path, "/proxy/") ||
		strings.HasSuffix(path, "/models") ||
		path == "/models"
}

func logAIServicesRequest(req *http.Request) {
	event := zerolog.Ctx(req.Context()).Info().
		Str("method", req.Method).
		Str("url", req.URL.Redacted()).
		Str("host", req.URL.Host).
		Str("path", req.URL.EscapedPath())
	if req.ContentLength >= 0 {
		event = event.Int64("content_length", req.ContentLength)
	}
	if contentType := req.Header.Get("Content-Type"); contentType != "" {
		event = event.Str("content_type", contentType)
	}
	if requestID := requestHeader(req.Header, "x-client-request-id", "x-request-id", "session_id"); requestID != "" {
		event = event.Str("request_id", requestID)
	}
	event.Msg("AI Services request")
}

func logAIServicesResponse(req *http.Request, resp *http.Response, duration time.Duration, err error) {
	event := zerolog.Ctx(req.Context()).Info()
	if err != nil || (resp != nil && resp.StatusCode >= 400) {
		event = zerolog.Ctx(req.Context()).Error()
	}
	event = event.
		Err(err).
		Str("method", req.Method).
		Str("url", req.URL.Redacted()).
		Str("host", req.URL.Host).
		Str("path", req.URL.EscapedPath()).
		Int64("duration_ms", duration.Milliseconds())
	if resp != nil {
		event = event.
			Int("status_code", resp.StatusCode).
			Str("status", resp.Status)
		if resp.ContentLength >= 0 {
			event = event.Int64("content_length", resp.ContentLength)
		}
		if contentType := resp.Header.Get("Content-Type"); contentType != "" {
			event = event.Str("content_type", contentType)
		}
		if responseID := requestHeader(resp.Header, "x-request-id", "request-id", "cf-ray"); responseID != "" {
			event = event.Str("response_id", responseID)
		}
		if retryAfter := requestHeader(resp.Header, "retry-after", "retry-after-ms"); retryAfter != "" {
			event = event.Str("retry_after", retryAfter)
		}
	}
	event.Msg("AI Services response")
}

func requestHeader(header http.Header, names ...string) string {
	for _, name := range names {
		if value := header.Get(name); value != "" {
			return value
		}
	}
	return ""
}
