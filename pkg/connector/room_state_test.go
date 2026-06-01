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

func TestRoomToolModesDefaultAndLegacyDisabled(t *testing.T) {
	if got := roomSearchMode(RoomConfig{}); got != toolModeBeeper {
		t.Fatalf("default search mode = %q", got)
	}
	if got := roomFetchMode(RoomConfig{}); got != toolModeBeeper {
		t.Fatalf("default fetch mode = %q", got)
	}
	if got := roomSearchMode(RoomConfig{DisabledTools: []string{"web_search"}}); got != toolModeOff {
		t.Fatalf("legacy disabled web_search should turn search off, got %q", got)
	}
	if got := roomFetchMode(RoomConfig{DisabledTools: []string{"fetch"}}); got != toolModeOff {
		t.Fatalf("disabled fetch should turn fetch off, got %q", got)
	}
	if got := roomSearchMode(RoomConfig{SearchMode: "native", DisabledTools: []string{"web_search"}}); got != toolModeNative {
		t.Fatalf("explicit search mode should win over disabled list, got %q", got)
	}
	if got := roomFetchMode(RoomConfig{FetchMode: "bad"}); got != defaultFetchMode {
		t.Fatalf("invalid fetch mode should fall back, got %q", got)
	}
}
