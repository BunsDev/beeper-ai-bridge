package connector

import (
	"testing"

	"github.com/beeper/ai-bridge/pkg/ai"
)

func TestAddBuiltInToolsToPayloadAddsCatalogBuiltIns(t *testing.T) {
	payload := map[string]any{
		"model": "openai/gpt-5.5",
		"tools": []any{map[string]any{
			"type": "function",
			"name": "lookup_contact",
		}},
	}

	next, changed := addBuiltInToolsToPayload(payload, []map[string]any{{"type": "image_generation"}})
	if !changed {
		t.Fatal("expected payload to change")
	}
	body := next.(map[string]any)
	if body["model"] != "openai/gpt-5.5" {
		t.Fatalf("expected model to stay unchanged, got %#v", body["model"])
	}
	if _, ok := body["tool_choice"]; ok {
		t.Fatalf("did not expect forced tool choice, got %#v", body["tool_choice"])
	}
	assertToolType(t, body["tools"], "function")
	assertToolType(t, body["tools"], "image_generation")
}

func TestAddBuiltInToolsToPayloadSkipsWithoutCatalogBuiltIns(t *testing.T) {
	payload := map[string]any{"model": "openai/gpt-5.3-chat", "input": "generate a photo of Amsterdam"}

	_, changed := addBuiltInToolsToPayload(payload, nil)
	if changed {
		t.Fatal("did not expect payload change without catalog built-ins")
	}
}

func TestAddBuiltInToolsToPayloadSkipsWhenAlreadyPresent(t *testing.T) {
	payload := map[string]any{
		"model": "openai/gpt-5.5",
		"tools": []any{map[string]any{
			"type": "image_generation",
		}},
	}

	_, changed := addBuiltInToolsToPayload(payload, []map[string]any{{"type": "image_generation"}})
	if changed {
		t.Fatal("did not expect payload change when built-in tool is already present")
	}
}

func TestAddBuiltInToolsToPayloadAddsOpenRouterBuiltIn(t *testing.T) {
	payload := map[string]any{"model": "anthropic/claude-sonnet-4.5"}

	next, changed := addBuiltInToolsToPayload(payload, []map[string]any{{"type": "openrouter:image_generation"}})
	if !changed {
		t.Fatal("expected payload to change")
	}
	assertToolType(t, next.(map[string]any)["tools"], "openrouter:image_generation")
}

func TestActiveBuiltInToolPayloadsHonorsNativeSearchMode(t *testing.T) {
	model := ai.Model{API: ai.ApiOpenAIResponses, Provider: ai.ProviderOpenAI, BuiltInTools: []string{"web_search", "image_generation"}}
	if got := activeBuiltInToolPayloads(model, RoomConfig{}); len(got) != 1 || got[0]["type"] != "image_generation" {
		t.Fatalf("default beeper search should suppress native web_search only, got %#v", got)
	}
	if got := activeBuiltInToolPayloads(model, RoomConfig{SearchMode: toolModeNative}); len(got) != 2 || got[0]["type"] != "web_search" || got[1]["type"] != "image_generation" {
		t.Fatalf("native search should allow provider web_search, got %#v", got)
	}
	if got := activeBuiltInToolPayloads(model, RoomConfig{SearchMode: toolModeOff}); len(got) != 1 || got[0]["type"] != "image_generation" {
		t.Fatalf("off search should suppress native web_search only, got %#v", got)
	}
}

func TestActiveBuiltInToolPayloadsHonorsNativeFetchMode(t *testing.T) {
	model := ai.Model{API: ai.ApiOpenAIResponses, Provider: ai.ProviderOpenRouter, BuiltInTools: []string{"web_fetch", "openrouter:image_generation"}}
	if got := activeBuiltInToolPayloads(model, RoomConfig{}); len(got) != 1 || got[0]["type"] != "openrouter:image_generation" {
		t.Fatalf("default beeper fetch should suppress native web_fetch only, got %#v", got)
	}
	if got := activeBuiltInToolPayloads(model, RoomConfig{FetchMode: toolModeNative}); len(got) != 2 || got[0]["type"] != "openrouter:web_fetch" || got[1]["type"] != "openrouter:image_generation" {
		t.Fatalf("native fetch should allow provider web_fetch, got %#v", got)
	}
	if got := activeBuiltInToolPayloads(model, RoomConfig{FetchMode: toolModeOff}); len(got) != 1 || got[0]["type"] != "openrouter:image_generation" {
		t.Fatalf("off fetch should suppress native web_fetch only, got %#v", got)
	}
}

func TestActiveBuiltInToolPayloadsInjectsNativeModesWithoutCatalogBuiltIns(t *testing.T) {
	model := ai.Model{API: ai.ApiGoogleGenerativeAI, Provider: ai.ProviderGoogle}
	got := activeBuiltInToolPayloads(model, RoomConfig{SearchMode: toolModeNative, FetchMode: toolModeNative})
	if len(got) != 2 || builtInToolKey(got[0]) != "google_search" || builtInToolKey(got[1]) != "url_context" {
		t.Fatalf("expected mode-driven native Google tools, got %#v", got)
	}
}

func TestNativeWebSearchToolPayloadsAreProviderSpecific(t *testing.T) {
	tests := []struct {
		name      string
		model     ai.Model
		wantKey   string
		wantValue any
	}{
		{
			name:      "openai responses",
			model:     ai.Model{API: ai.ApiOpenAIResponses, Provider: ai.ProviderOpenAI, BuiltInTools: []string{"web_search"}},
			wantKey:   "type",
			wantValue: "web_search",
		},
		{
			name:      "openrouter",
			model:     ai.Model{API: ai.ApiOpenAIResponses, Provider: ai.ProviderOpenRouter, BuiltInTools: []string{"web_search"}},
			wantKey:   "type",
			wantValue: "openrouter:web_search",
		},
		{
			name:      "anthropic",
			model:     ai.Model{API: ai.ApiAnthropicMessages, Provider: ai.ProviderAnthropic, BuiltInTools: []string{"web_search"}},
			wantKey:   "type",
			wantValue: "web_search_20250305",
		},
		{
			name:      "google vertex",
			model:     ai.Model{API: ai.ApiGoogleVertex, Provider: ai.ProviderGoogleVertex, BuiltInTools: []string{"web_search"}},
			wantKey:   "google_search",
			wantValue: map[string]any{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := activeBuiltInToolPayloads(tt.model, RoomConfig{SearchMode: toolModeNative})
			if len(got) != 1 {
				t.Fatalf("expected one native search payload, got %#v", got)
			}
			if tt.wantKey == "google_search" {
				if _, ok := got[0]["google_search"].(map[string]any); !ok {
					t.Fatalf("expected google_search object, got %#v", got[0])
				}
				return
			}
			if got[0][tt.wantKey] != tt.wantValue {
				t.Fatalf("unexpected payload %#v", got[0])
			}
		})
	}
}

func TestNativeWebFetchToolPayloadsAreProviderSpecific(t *testing.T) {
	tests := []struct {
		name      string
		model     ai.Model
		wantKey   string
		wantValue any
	}{
		{
			name:      "openai responses unsupported",
			model:     ai.Model{API: ai.ApiOpenAIResponses, Provider: ai.ProviderOpenAI},
			wantKey:   "",
			wantValue: nil,
		},
		{
			name:      "openrouter responses",
			model:     ai.Model{API: ai.ApiOpenAIResponses, Provider: ai.ProviderOpenRouter},
			wantKey:   "type",
			wantValue: "openrouter:web_fetch",
		},
		{
			name:      "openrouter completions",
			model:     ai.Model{API: ai.ApiOpenAICompletions, Provider: ai.ProviderOpenRouter},
			wantKey:   "type",
			wantValue: "openrouter:web_fetch",
		},
		{
			name:      "anthropic",
			model:     ai.Model{API: ai.ApiAnthropicMessages, Provider: ai.ProviderAnthropic},
			wantKey:   "type",
			wantValue: "web_fetch_20250910",
		},
		{
			name:      "google",
			model:     ai.Model{API: ai.ApiGoogleGenerativeAI, Provider: ai.ProviderGoogle},
			wantKey:   "url_context",
			wantValue: map[string]any{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := activeBuiltInToolPayloads(tt.model, RoomConfig{FetchMode: toolModeNative})
			if tt.wantKey == "" {
				if len(got) != 0 {
					t.Fatalf("expected no native fetch payload, got %#v", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("expected one native fetch payload, got %#v", got)
			}
			if tt.wantKey == "url_context" {
				if _, ok := got[0]["url_context"].(map[string]any); !ok {
					t.Fatalf("expected url_context object, got %#v", got[0])
				}
				return
			}
			if got[0][tt.wantKey] != tt.wantValue {
				t.Fatalf("unexpected payload %#v", got[0])
			}
		})
	}
}

func assertToolType(t *testing.T, raw any, toolType string) {
	t.Helper()
	assertToolTypeCount(t, raw, toolType, 1)
}

func assertToolTypeCount(t *testing.T, raw any, toolType string, want int) {
	t.Helper()
	tools, ok := raw.([]any)
	if !ok {
		t.Fatalf("expected tools array, got %#v", raw)
	}
	count := 0
	for _, tool := range tools {
		toolMap, ok := tool.(map[string]any)
		if ok && toolMap["type"] == toolType {
			count++
		}
	}
	if count != want {
		t.Fatalf("tool %q count = %d, want %d in %#v", toolType, count, want, raw)
	}
}
