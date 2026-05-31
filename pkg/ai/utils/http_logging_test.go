package utils

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestAIServicesLoggingTransportLogsMetadataWithoutBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxy/openai/v1/responses" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", "req_123")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"secret-response"}`))
	}))
	defer server.Close()

	var logs bytes.Buffer
	logger := zerolog.New(&logs)
	ctx := logger.WithContext(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/proxy/openai/v1/responses", strings.NewReader("secret-request"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("X-Client-Request-ID", "session_123")

	resp, err := WithAIServicesLogging(&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	output := logs.String()
	if !strings.Contains(output, `"message":"Sending AI Services HTTP request"`) || !strings.Contains(output, `"message":"AI Services HTTP request returned error status"`) {
		t.Fatalf("missing request/response logs:\n%s", output)
	}
	if !strings.Contains(output, `"action":"ai_services_http"`) || !strings.Contains(output, `"request_number":`) {
		t.Fatalf("missing request context:\n%s", output)
	}
	if !strings.Contains(output, `"status_code":401`) || !strings.Contains(output, `"level":"error"`) || !strings.Contains(output, `"duration":`) {
		t.Fatalf("missing error response metadata:\n%s", output)
	}
	for _, secret := range []string{"secret-request", "secret-response", "secret-token"} {
		if strings.Contains(output, secret) {
			t.Fatalf("log output contains secret %q:\n%s", secret, output)
		}
	}
}

func TestIsAIServicesRequestIncludesLimits(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.test/dev/limits", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !IsAIServicesRequest(req) {
		t.Fatal("expected /limits to be logged as an AI Services request")
	}
}
