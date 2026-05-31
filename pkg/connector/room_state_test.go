package connector

import (
	"testing"

	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func TestApplyRoomModelConfigUsesProviderModelRefAndReasoning(t *testing.T) {
	config := RoomConfig{}
	applyRoomModelConfig(&config, map[string]any{"model": "openrouter/openai/gpt-5", "reasoning": "high"})
	if config.ProviderID != "openrouter" || config.ModelID != "openai/gpt-5" || config.ThinkingLevel != "high" {
		t.Fatalf("unexpected model config %#v", config)
	}
}

func TestRoomModelStateContentIncludesCatalogName(t *testing.T) {
	content := roomModelStateContent(ai.Model{ID: "openai/gpt-5.5", Name: "GPT-5.5"}, "beeper/openai/gpt-5.5", "medium")
	if content["model"] != "beeper/openai/gpt-5.5" || content["name"] != "GPT-5.5" || content["reasoning"] != "medium" {
		t.Fatalf("unexpected room model content %#v", content)
	}
}

func TestStringSliceDeduplicatesDisabledTools(t *testing.T) {
	got := stringSlice([]any{"web_search", "web_search", "", "fetch"})
	if len(got) != 2 || got[0] != "web_search" || got[1] != "fetch" {
		t.Fatalf("unexpected disabled tools %#v", got)
	}
}
