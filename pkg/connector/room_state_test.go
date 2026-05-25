package connector

import "testing"

func TestApplyRoomModelConfigUsesProviderModelRefAndReasoning(t *testing.T) {
	config := RoomConfig{}
	applyRoomModelConfig(&config, map[string]any{"model": "openrouter/openai/gpt-5", "reasoning": "high"})
	if config.ProviderID != "openrouter" || config.ModelID != "openai/gpt-5" || config.ThinkingLevel != "high" {
		t.Fatalf("unexpected model config %#v", config)
	}
}

func TestStringSliceDeduplicatesDisabledTools(t *testing.T) {
	got := stringSlice([]any{"web_search", "web_search", "", "fetch"})
	if len(got) != 2 || got[0] != "web_search" || got[1] != "fetch" {
		t.Fatalf("unexpected disabled tools %#v", got)
	}
}
