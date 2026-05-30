package connector

import (
	"testing"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"github.com/beeper/ai-bridge/pkg/msgconv"
)

func TestProviderBuiltInToolsPayloadRewritesOpenAIImageModelToHostedTool(t *testing.T) {
	provider := aiid.ProviderConfig{Models: []ai.Model{
		{ID: "openai/gpt-5.5", API: ai.ApiOpenAIResponses, Provider: ai.ProviderOpenAI},
		{ID: "openai/gpt-image-2", API: ai.ApiOpenAIResponses, Provider: ai.ProviderOpenAI},
	}}
	model := ai.Model{ID: "openai/gpt-image-2", API: ai.ApiOpenAIResponses, Provider: ai.ProviderOpenAI}
	payload := map[string]any{"model": "openai/gpt-image-2", "input": "create an image of Amsterdam"}

	next, changed := providerBuiltInToolsPayload(provider, model, model, msgconv.MatrixPrompt{Text: "create an image of Amsterdam"}, payload)
	if !changed {
		t.Fatal("expected payload to change")
	}
	body := next.(map[string]any)
	if body["model"] != "openai/gpt-5.5" {
		t.Fatalf("expected hosted tool model, got %#v", body["model"])
	}
	if choice, ok := body["tool_choice"].(map[string]any); !ok || choice["type"] != "image_generation" {
		t.Fatalf("expected forced image tool choice, got %#v", body["tool_choice"])
	}
	assertToolType(t, body["tools"], "image_generation")
}

func TestProviderBuiltInToolsPayloadAddsImageToolForOpenAIPrompt(t *testing.T) {
	model := ai.Model{ID: "openai/gpt-5.5", API: ai.ApiOpenAIResponses, Provider: ai.ProviderOpenAI}
	payload := map[string]any{"model": "openai/gpt-5.5", "input": "generate a photo of Amsterdam"}

	next, changed := providerBuiltInToolsPayload(aiid.ProviderConfig{}, model, model, msgconv.MatrixPrompt{Text: "generate a photo of Amsterdam"}, payload)
	if !changed {
		t.Fatal("expected payload to change")
	}
	body := next.(map[string]any)
	if body["model"] != "openai/gpt-5.5" {
		t.Fatalf("expected model to stay unchanged, got %#v", body["model"])
	}
	if _, ok := body["tool_choice"]; ok {
		t.Fatalf("did not expect forced tool choice for normal model prompt, got %#v", body["tool_choice"])
	}
	assertToolType(t, body["tools"], "image_generation")
}

func TestProviderBuiltInToolsPayloadAddsOpenRouterImageTool(t *testing.T) {
	model := ai.Model{ID: "anthropic/claude-sonnet-4.5", API: ai.ApiOpenAIResponses, Provider: ai.ProviderOpenRouter}
	payload := map[string]any{"model": "anthropic/claude-sonnet-4.5", "input": "create an image of Amsterdam"}

	next, changed := providerBuiltInToolsPayload(aiid.ProviderConfig{}, model, model, msgconv.MatrixPrompt{Text: "create an image of Amsterdam"}, payload)
	if !changed {
		t.Fatal("expected payload to change")
	}
	body := next.(map[string]any)
	assertToolType(t, body["tools"], "openrouter:image_generation")
}

func assertToolType(t *testing.T, raw any, toolType string) {
	t.Helper()
	tools, ok := raw.([]any)
	if !ok {
		t.Fatalf("expected tools array, got %#v", raw)
	}
	for _, tool := range tools {
		toolMap, ok := tool.(map[string]any)
		if ok && toolMap["type"] == toolType {
			return
		}
	}
	t.Fatalf("tool %q not found in %#v", toolType, raw)
}
