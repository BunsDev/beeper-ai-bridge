package providers

import (
	"testing"

	ai "github.com/earendil-works/pi-mono/packages/ai/src"
)

func TestTransformMessagesExportsProviderUtility(t *testing.T) {
	model := ai.Model{ID: "gpt-test", API: ai.ApiOpenAICompletions, Provider: ai.ProviderOpenAI, Input: []string{"text"}}
	messages := []ai.Message{
		{Role: "user", Content: []ai.ContentBlock{{Type: "image", MimeType: "image/png", Data: "abc"}}},
		{Role: "assistant", API: ai.ApiOpenAIResponses, Provider: ai.ProviderOpenAI, Model: "other", StopReason: ai.StopReasonToolUse, Content: []ai.ContentBlock{{Type: "toolCall", ID: "call/raw", Name: "read", Arguments: map[string]any{}}}},
	}
	transformed := TransformMessages(messages, model, func(id string, model ai.Model, source ai.Message) string {
		return "call_normalized"
	})
	userContent := transformed[0].Content.([]ai.ContentBlock)
	if len(userContent) != 1 || userContent[0].Text != nonVisionUserImagePlaceholder {
		t.Fatalf("expected unsupported image placeholder, got %#v", transformed[0].Content)
	}
	assistantBlocks := transformed[1].Content.([]ai.ContentBlock)
	if assistantBlocks[0].ID != "call_normalized" {
		t.Fatalf("expected normalized tool call id, got %#v", assistantBlocks[0])
	}
	if len(transformed) != 3 || transformed[2].Role != "toolResult" || transformed[2].ToolCallID != "call_normalized" {
		t.Fatalf("expected synthetic tool result for normalized call, got %#v", transformed)
	}
}

func TestTransformMessagesPreservesStringUserContentForNonVisionModels(t *testing.T) {
	model := ai.Model{ID: "gpt-test", API: ai.ApiOpenAIResponses, Provider: ai.ProviderOpenAI, Input: []string{"text"}}
	transformed := TransformMessages([]ai.Message{{Role: "user", Content: "hello"}}, model, nil)
	if len(transformed) != 1 || transformed[0].Content != "hello" {
		t.Fatalf("expected string user content to be preserved, got %#v", transformed)
	}
}
