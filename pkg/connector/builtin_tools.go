package connector

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/beeper/ai-bridge/pkg/agent/harness"
	"github.com/beeper/ai-bridge/pkg/ai"
)

const anthropicWebFetchBetaMetadataKey = "anthropic_web_fetch_beta"

func (cl *Client) registerProviderBuiltInToolHooks(agentHarness *harness.AgentHarness, roomConfig RoomConfig) {
	if agentHarness == nil {
		return
	}
	agentHarness.On("before_provider_request", func(_ context.Context, event harness.AgentHarnessEvent) (any, error) {
		if event.Model == nil || roomFetchMode(roomConfig) != toolModeNative || event.Model.API != ai.ApiAnthropicMessages {
			return nil, nil
		}
		if !modelSupportsBuiltInTool(*event.Model, "web_fetch") {
			return nil, nil
		}
		if _, ok := nativeWebFetchToolPayload(*event.Model); !ok {
			return nil, nil
		}
		return harness.BeforeProviderRequestResult{
			StreamOptions: harness.AgentHarnessStreamOptions{
				Metadata: map[string]any{anthropicWebFetchBetaMetadataKey: true},
			},
		}, nil
	})
	agentHarness.On("before_provider_payload", func(_ context.Context, event harness.AgentHarnessEvent) (any, error) {
		if event.Model == nil {
			return nil, nil
		}
		payload, changed := addBuiltInToolsToPayload(event.Payload, activeBuiltInToolPayloads(*event.Model, roomConfig))
		if !changed {
			return nil, nil
		}
		return harness.BeforeProviderPayloadResult{Payload: payload}, nil
	})
}

func activeBuiltInToolPayloads(model ai.Model, roomConfig RoomConfig) []map[string]any {
	out := make([]map[string]any, 0, len(model.BuiltInTools)+2)
	if roomSearchMode(roomConfig) == toolModeNative && modelSupportsBuiltInTool(model, "web_search") {
		if payload, ok := nativeWebSearchToolPayload(model); ok {
			out = appendBuiltInToolPayload(out, payload)
		}
	}
	if roomFetchMode(roomConfig) == toolModeNative && modelSupportsBuiltInTool(model, "web_fetch") {
		if payload, ok := nativeWebFetchToolPayload(model); ok {
			out = appendBuiltInToolPayload(out, payload)
		}
	}
	for _, tool := range model.BuiltInTools {
		payload, ok := builtInToolPayload(model, roomConfig, tool)
		if !ok {
			continue
		}
		out = appendBuiltInToolPayload(out, payload)
	}
	return out
}

func modelSupportsBuiltInTool(model ai.Model, tool string) bool {
	canonical := normalizedBuiltInTool(tool)
	for _, supported := range model.BuiltInTools {
		if normalizedBuiltInTool(supported) == canonical {
			return true
		}
	}
	return false
}

func builtInToolPayload(model ai.Model, roomConfig RoomConfig, tool string) (map[string]any, bool) {
	switch normalizedBuiltInTool(tool) {
	case "web_search":
		if roomSearchMode(roomConfig) != toolModeNative {
			return nil, false
		}
		return nativeWebSearchToolPayload(model)
	case "web_fetch":
		if roomFetchMode(roomConfig) != toolModeNative {
			return nil, false
		}
		return nativeWebFetchToolPayload(model)
	case "image_generation":
		switch {
		case strings.HasPrefix(strings.TrimSpace(tool), "openrouter:"):
			return map[string]any{"type": strings.TrimSpace(tool)}, true
		case model.Provider == ai.ProviderOpenAI || model.Provider == ai.ProviderOpenRouter:
			return map[string]any{"type": "image_generation"}, true
		default:
			return nil, false
		}
	default:
		return nil, false
	}
}

func normalizedBuiltInTool(tool string) string {
	tool = strings.ToLower(strings.TrimSpace(tool))
	switch tool {
	case "web_search", "web_search_preview", "openrouter:web_search", "web_search_20250305", "google_search", "google_search_retrieval":
		return "web_search"
	case "web_fetch", "openrouter:web_fetch", "web_fetch_20250910", "web_fetch_20260209", "url_context", "urlcontext":
		return "web_fetch"
	case "image_generation", "openrouter:image_generation":
		return "image_generation"
	default:
		return tool
	}
}

func nativeWebSearchToolPayload(model ai.Model) (map[string]any, bool) {
	switch model.API {
	case ai.ApiAnthropicMessages:
		return map[string]any{"type": "web_search_20250305", "name": "web_search"}, true
	case ai.ApiGoogleGenerativeAI, ai.ApiGoogleVertex:
		return map[string]any{"google_search": map[string]any{}}, true
	case ai.ApiOpenAIResponses:
		if model.Provider == ai.ProviderOpenRouter {
			return map[string]any{"type": "openrouter:web_search"}, true
		}
		return map[string]any{"type": "web_search"}, true
	case ai.ApiOpenAICompletions:
		if model.Provider == ai.ProviderOpenRouter {
			return map[string]any{"type": "openrouter:web_search"}, true
		}
		return nil, false
	default:
		return nil, false
	}
}

func nativeWebFetchToolPayload(model ai.Model) (map[string]any, bool) {
	switch model.API {
	case ai.ApiAnthropicMessages:
		return map[string]any{"type": "web_fetch_20250910", "name": "web_fetch", "citations": map[string]any{"enabled": true}}, true
	case ai.ApiGoogleGenerativeAI, ai.ApiGoogleVertex:
		return map[string]any{"url_context": map[string]any{}}, true
	case ai.ApiOpenAIResponses, ai.ApiOpenAICompletions:
		if model.Provider == ai.ProviderOpenRouter {
			return map[string]any{"type": "openrouter:web_fetch"}, true
		}
		return nil, false
	default:
		return nil, false
	}
}

func appendBuiltInToolPayload(payloads []map[string]any, payload map[string]any) []map[string]any {
	key := builtInToolKey(payload)
	if key == "" {
		return payloads
	}
	for _, existing := range payloads {
		if builtInToolKey(existing) == key {
			return payloads
		}
	}
	return append(payloads, payload)
}

func addBuiltInToolsToPayload(payload any, builtInTools []map[string]any) (any, bool) {
	body, ok := payload.(map[string]any)
	if !ok || len(builtInTools) == 0 {
		return payload, false
	}

	next := clonePayloadMap(body)
	tools := toolsAsAny(next["tools"])
	changed := false
	for _, toolPayload := range builtInTools {
		before := len(tools)
		tools = appendBuiltInTool(tools, toolPayload)
		changed = changed || len(tools) != before
	}
	if !changed {
		return payload, false
	}
	next["tools"] = tools
	return next, true
}

func appendBuiltInTool(tools []any, toolPayload map[string]any) []any {
	toolKey := builtInToolKey(toolPayload)
	if toolKey == "" {
		return tools
	}
	for _, tool := range tools {
		if toolMap, ok := tool.(map[string]any); ok {
			if builtInToolKey(toolMap) == toolKey {
				return tools
			}
			if toolMap["type"] == "function" && toolMap["name"] == toolKey {
				return tools
			}
		}
	}
	return append(tools, maps.Clone(toolPayload))
}

func builtInToolKey(tool map[string]any) string {
	if tool == nil {
		return ""
	}
	for _, key := range []string{"type", "name"} {
		if value := strings.TrimSpace(stringFromAny(tool[key])); value != "" {
			return value
		}
	}
	if _, ok := tool["google_search"]; ok {
		return "google_search"
	}
	if _, ok := tool["googleSearch"]; ok {
		return "google_search"
	}
	if _, ok := tool["url_context"]; ok {
		return "url_context"
	}
	if _, ok := tool["urlContext"]; ok {
		return "url_context"
	}
	return ""
}

func toolsAsAny(raw any) []any {
	switch tools := raw.(type) {
	case []any:
		return slices.Clone(tools)
	case []map[string]any:
		out := make([]any, 0, len(tools))
		for _, tool := range tools {
			out = append(out, tool)
		}
		return out
	default:
		return nil
	}
}

func clonePayloadMap(in map[string]any) map[string]any {
	return maps.Clone(in)
}
