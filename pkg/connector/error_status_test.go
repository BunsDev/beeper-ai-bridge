package connector

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"maunium.net/go/mautrix/event"
)

type testHTTPStatusError struct {
	StatusCode int
	message    string
}

func (err testHTTPStatusError) Error() string {
	return err.message
}

func TestMatrixMessageStatusForAIErrorMapsUnsupportedPreflight(t *testing.T) {
	status := matrixMessageStatusForAIError(errors.New("model gpt-5.4 does not support image input"))
	if status.Status != event.MessageStatusFail || status.ErrorReason != event.MessageStatusUnsupported {
		t.Fatalf("expected permanent unsupported status, got %#v", status)
	}
	if status.Message != "model gpt-5.4 does not support image input" {
		t.Fatalf("expected specific user message, got %q", status.Message)
	}
}

func TestMatrixMessageStatusForAIErrorMapsAuthFailure(t *testing.T) {
	status := matrixMessageStatusForAIError(errors.New("missing API key for provider local"))
	if status.Status != event.MessageStatusFail || status.ErrorReason != event.MessageStatusNoPermission {
		t.Fatalf("expected permanent no-permission status, got %#v", status)
	}
	if status.Message == "" || status.Message == status.InternalError.Error() {
		t.Fatalf("expected sanitized auth message, got %#v", status)
	}
}

func TestMatrixMessageStatusForAIErrorMapsHTTPProviderFailures(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus event.MessageStatus
		wantReason event.MessageStatusReason
	}{
		{
			name:       "rate limit",
			err:        testHTTPStatusError{StatusCode: http.StatusTooManyRequests, message: "rate limited"},
			wantStatus: event.MessageStatusRetriable,
			wantReason: event.MessageStatusNetworkError,
		},
		{
			name:       "bad request",
			err:        testHTTPStatusError{StatusCode: http.StatusBadRequest, message: "bad request"},
			wantStatus: event.MessageStatusFail,
			wantReason: event.MessageStatusUnsupported,
		},
		{
			name:       "wrapped server error",
			err:        fmt.Errorf("provider failed: %w", testHTTPStatusError{StatusCode: http.StatusBadGateway, message: "bad gateway"}),
			wantStatus: event.MessageStatusRetriable,
			wantReason: event.MessageStatusNetworkError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := matrixMessageStatusForAIError(tt.err)
			if status.Status != tt.wantStatus || status.ErrorReason != tt.wantReason {
				t.Fatalf("unexpected status %#v", status)
			}
		})
	}
}

func TestMatrixMessageStatusForAIErrorMapsCancellation(t *testing.T) {
	status := matrixMessageStatusForAIError(context.Canceled)
	if status.Status != event.MessageStatusFail || status.ErrorReason != event.MessageStatusBridgeUnavailable {
		t.Fatalf("expected bridge-unavailable cancellation status, got %#v", status)
	}
}
