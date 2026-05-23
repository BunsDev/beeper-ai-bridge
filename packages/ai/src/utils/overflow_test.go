package utils

import (
	"testing"

	ai "github.com/earendil-works/pi-mono/packages/ai/src"
)

func TestIsContextOverflowFromErrorMessage(t *testing.T) {
	message := ai.Message{StopReason: ai.StopReasonError, ErrorMessage: "Your input exceeds the context window of this model"}
	if !IsContextOverflow(message) {
		t.Fatal("expected OpenAI overflow error to be detected")
	}
	nonOverflow := ai.Message{StopReason: ai.StopReasonError, ErrorMessage: "rate limit: too many tokens, please wait"}
	if IsContextOverflow(nonOverflow) {
		t.Fatal("expected rate limit to be excluded")
	}
}

func TestIsContextOverflowFromUsage(t *testing.T) {
	message := ai.Message{StopReason: ai.StopReasonStop, Usage: ai.Usage{Input: 90, CacheRead: 20}}
	if !IsContextOverflow(message, 100) {
		t.Fatal("expected silent overflow from usage")
	}
	length := ai.Message{StopReason: ai.StopReasonLength, Usage: ai.Usage{Input: 99, Output: 0}}
	if !IsContextOverflow(length, 100) {
		t.Fatal("expected length stop overflow")
	}
	if len(GetOverflowPatterns()) == 0 {
		t.Fatal("expected exported overflow patterns")
	}
}
