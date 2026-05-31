package utils

import (
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

var aiServicesHTTPRequestCounter atomic.Int64

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

	log := aiServicesRequestLogger(req)
	ctx := log.WithContext(req.Context())
	req = req.WithContext(ctx)

	log.Trace().Msg("Sending AI Services HTTP request")
	started := time.Now()
	resp, err := base.RoundTrip(req)
	duration := time.Since(started)
	logAIServicesResponse(log, resp, duration, err)
	return resp, err
}

func aiServicesRequestLogger(req *http.Request) zerolog.Logger {
	logCtx := zerolog.Ctx(req.Context()).With().
		Str("action", "ai_services_http").
		Int64("request_number", aiServicesHTTPRequestCounter.Add(1)).
		Str("method", req.Method).
		Str("url", req.URL.Redacted()).
		Str("host", req.URL.Host).
		Str("path", req.URL.EscapedPath())
	if req.ContentLength >= 0 {
		logCtx = logCtx.Int64("content_length", req.ContentLength)
	}
	if contentType := req.Header.Get("Content-Type"); contentType != "" {
		logCtx = logCtx.Str("content_type", contentType)
	}
	if requestID := requestHeader(req.Header, "x-client-request-id", "x-request-id", "session_id"); requestID != "" {
		logCtx = logCtx.Str("request_id", requestID)
	}
	return logCtx.Logger()
}

func logAIServicesResponse(log zerolog.Logger, resp *http.Response, duration time.Duration, err error) {
	if err != nil {
		log.Err(err).
			Dur("duration", duration).
			Msg("AI Services HTTP request failed")
		return
	}
	if resp == nil {
		log.Error().
			Dur("duration", duration).
			Msg("AI Services HTTP request returned no response")
		return
	}

	logEvt := log.Debug()
	if resp.StatusCode >= 400 {
		logEvt = log.Error()
	}
	logEvt = logEvt.
		Dur("duration", duration).
		Int("status_code", resp.StatusCode).
		Str("status", resp.Status)
	if resp.ContentLength >= 0 {
		logEvt = logEvt.Int64("response_content_length", resp.ContentLength)
	}
	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		logEvt = logEvt.Str("response_content_type", contentType)
	}
	if responseID := requestHeader(resp.Header, "x-request-id", "request-id", "cf-ray"); responseID != "" {
		logEvt = logEvt.Str("response_id", responseID)
	}
	if retryAfter := requestHeader(resp.Header, "retry-after", "retry-after-ms"); retryAfter != "" {
		logEvt = logEvt.Str("retry_after", retryAfter)
	}
	if resp.StatusCode >= 400 {
		logEvt.Msg("AI Services HTTP request returned error status")
	} else {
		logEvt.Msg("AI Services HTTP request completed")
	}
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
		path == "/models" ||
		strings.HasSuffix(path, "/limits") ||
		path == "/limits"
}

func requestHeader(header http.Header, names ...string) string {
	for _, name := range names {
		if value := header.Get(name); value != "" {
			return value
		}
	}
	return ""
}
