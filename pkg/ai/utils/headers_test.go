package utils

import (
	"net/http"
	"testing"
)

func TestHeadersToRecord(t *testing.T) {
	headers := http.Header{}
	headers.Add("x-test", "one")
	headers.Add("x-test", "two")
	headers.Set("content-type", "application/json")

	record := HeadersToRecord(headers)
	if record["X-Test"] != "one" {
		t.Fatalf("expected first header value, got %#v", record)
	}
	if record["Content-Type"] != "application/json" {
		t.Fatalf("expected content type, got %#v", record)
	}
}
