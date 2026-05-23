package utils

import (
	"errors"
	"testing"

	ai "github.com/earendil-works/pi-mono/packages/ai/src"
)

func TestAssistantMessageDiagnostics(t *testing.T) {
	err := errors.New("boom")
	diagnostic := CreateAssistantMessageDiagnostic("provider_transport_failure", err, map[string]interface{}{"phase": "before_message_stream_start"})
	if diagnostic.Type != "provider_transport_failure" || diagnostic.Error == nil || diagnostic.Error.Message != "boom" {
		t.Fatalf("unexpected diagnostic %#v", diagnostic)
	}
	message := ai.Message{Role: "assistant"}
	AppendAssistantMessageDiagnostic(&message, diagnostic)
	if len(message.Diagnostics) != 1 {
		t.Fatalf("expected appended diagnostic, got %#v", message.Diagnostics)
	}
	if FormatThrownValue("text") != "text" {
		t.Fatalf("expected string thrown value")
	}
}
