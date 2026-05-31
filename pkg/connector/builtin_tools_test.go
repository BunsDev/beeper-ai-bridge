package connector

import "testing"

func TestAddBuiltInToolsToPayloadAddsCatalogBuiltIns(t *testing.T) {
	payload := map[string]any{
		"model": "openai/gpt-5.5",
		"tools": []any{map[string]any{
			"type": "function",
			"name": "lookup_contact",
		}},
	}

	next, changed := addBuiltInToolsToPayload(payload, []string{"image_generation"})
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

	_, changed := addBuiltInToolsToPayload(payload, []string{"image_generation"})
	if changed {
		t.Fatal("did not expect payload change when built-in tool is already present")
	}
}

func TestAddBuiltInToolsToPayloadAddsOpenRouterBuiltIn(t *testing.T) {
	payload := map[string]any{"model": "anthropic/claude-sonnet-4.5"}

	next, changed := addBuiltInToolsToPayload(payload, []string{"openrouter:image_generation"})
	if !changed {
		t.Fatal("expected payload to change")
	}
	assertToolType(t, next.(map[string]any)["tools"], "openrouter:image_generation")
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
