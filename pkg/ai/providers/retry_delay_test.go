package providers

import (
	"net/http"
	"testing"
	"time"
)

func TestRequestedRetryDelayParsesHeaders(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	response := &http.Response{Header: http.Header{"Retry-After-Ms": []string{"1500"}}}
	delay, ok := RequestedRetryDelay(response, now)
	if !ok || delay != 1500*time.Millisecond {
		t.Fatalf("expected retry-after-ms delay, got %s ok=%v", delay, ok)
	}
	response = &http.Response{Header: http.Header{"Retry-After": []string{"2.5"}}}
	delay, ok = RequestedRetryDelay(response, now)
	if !ok || delay != 2500*time.Millisecond {
		t.Fatalf("expected retry-after seconds delay, got %s ok=%v", delay, ok)
	}
	response = &http.Response{Header: http.Header{"Retry-After": []string{now.Add(3 * time.Second).Format(http.TimeFormat)}}}
	delay, ok = RequestedRetryDelay(response, now)
	if !ok || delay != 3*time.Second {
		t.Fatalf("expected retry-after date delay, got %s ok=%v", delay, ok)
	}
}

func TestRetryDelayExceedsCap(t *testing.T) {
	capMs := 1000
	if !RetryDelayExceedsCap(1500*time.Millisecond, &capMs) {
		t.Fatal("expected retry delay to exceed cap")
	}
	if RetryDelayExceedsCap(500*time.Millisecond, &capMs) {
		t.Fatal("expected retry delay within cap")
	}
	disabled := 0
	if RetryDelayExceedsCap(24*time.Hour, &disabled) {
		t.Fatal("expected zero cap to disable limit")
	}
	if got := RetryDelayCapError(1500*time.Millisecond, capMs); got != "Retry delay 1500ms exceeds maxRetryDelayMs 1000" {
		t.Fatalf("unexpected cap error %q", got)
	}
}
