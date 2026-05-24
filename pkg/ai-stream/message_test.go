package aistream

import (
	"testing"

	agui "github.com/beeper/ai-bridge/pkg/ag-ui"
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func TestInitialMessageExtraIncludesBeeperAIEnvelope(t *testing.T) {
	extra := InitialMessageExtra(RunInfo{
		AgentDisplayName: "GPT-5",
		AgentID:          "assistant:beeper:gpt-5",
		MessageID:        "assistant:run",
		ModelID:          "gpt-5",
		ProviderID:       "beeper",
		RunID:            "run",
		ThreadID:         "thread",
	})
	aiMessage := extra[AIContentKey].(map[string]any)
	if aiMessage["id"] != "assistant:run" || aiMessage["role"] != "assistant" {
		t.Fatalf("unexpected AI message %#v", aiMessage)
	}
	metadata := extra[AIMetadataKey].(map[string]any)
	if metadata["protocol"] != "ag-ui" || metadata["model"] != "beeper/gpt-5" {
		t.Fatalf("unexpected metadata %#v", metadata)
	}
}

func TestStreamMapperBuildsCarrierContent(t *testing.T) {
	mapper := NewStreamMapper(RunInfo{MessageID: "assistant:run", RunID: "run", ThreadID: "thread"})
	content := mapper.CarrierContent(ai.AssistantMessageEvent{Type: "text_delta", Delta: "hi"}, "$event")
	if _, ok := content["room_id"]; ok {
		t.Fatalf("carrier content must not override room_id: %#v", content)
	}
	if _, ok := content["event_id"]; ok {
		t.Fatalf("carrier content must not override event_id: %#v", content)
	}
	deltas := content[LLMStreamDeltasKey].([]map[string]any)
	if len(deltas) != 2 {
		t.Fatalf("expected text start and content deltas, got %#v", deltas)
	}
	part := deltas[1]["part"].(map[string]any)
	if part["type"] != agui.TextMessageContent || part["delta"] != "hi" {
		t.Fatalf("unexpected text part %#v", part)
	}
}

func TestFinalMessageContentIncludesUsageAndEditTopLevel(t *testing.T) {
	content, extra, topLevel := FinalMessageContent(RunInfo{
		AgentDisplayName: "GPT-5",
		AgentID:          "assistant:beeper:gpt-5",
		MessageID:        "assistant:run",
		ModelID:          "gpt-5",
		ProviderID:       "beeper",
		RunID:            "run",
		ThreadID:         "thread",
	}, ai.Message{
		Role:       "assistant",
		Content:    []ai.ContentBlock{{Type: "text", Text: "**ok**"}},
		StopReason: ai.StopReasonStop,
		Usage:      ai.Usage{Input: 1, Output: 2},
	})
	if content.Body != "**ok**" || content.FormattedBody == "" {
		t.Fatalf("unexpected Matrix content %#v", content)
	}
	if topLevel[DontRenderEditedKey] != true {
		t.Fatalf("missing dont-render-edited top-level content %#v", topLevel)
	}
	metadata := extra[AIMetadataKey].(map[string]any)
	usage := metadata["usage"].(map[string]any)
	if usage["promptTokens"] != 1 || usage["completionTokens"] != 2 || usage["totalTokens"] != 3 {
		t.Fatalf("unexpected usage %#v", usage)
	}
}

func TestFinalMessageContentShowsErrorMessageForEmptyContent(t *testing.T) {
	content, extra, _ := FinalMessageContent(RunInfo{
		AgentDisplayName: "GPT-5",
		AgentID:          "assistant:beeper:gpt-5",
		MessageID:        "assistant:run",
		ModelID:          "gpt-5",
		ProviderID:       "beeper",
		RunID:            "run",
		ThreadID:         "thread",
	}, ai.Message{
		Role:         "assistant",
		Content:      []ai.ContentBlock{},
		StopReason:   ai.StopReasonError,
		ErrorMessage: "upstream failed",
	})
	if content.Body != "upstream failed" {
		t.Fatalf("expected visible error message, got %#v", content.Body)
	}
	aiMessage := extra[AIContentKey].(map[string]any)
	parts := aiMessage["parts"].([]map[string]any)
	if len(parts) != 1 || parts[0]["content"] != "upstream failed" {
		t.Fatalf("expected error text part, got %#v", parts)
	}
	metadata := extra[AIMetadataKey].(map[string]any)
	status := metadata["status"].(map[string]any)
	statusError := status["error"].(map[string]any)
	if status["finishReason"] != "error" || statusError["message"] != "upstream failed" {
		t.Fatalf("unexpected error status %#v", status)
	}
	preview := metadata["preview"].(map[string]any)
	if preview["text"] != "upstream failed" {
		t.Fatalf("unexpected preview %#v", preview)
	}
}
