package connector

import (
	"context"
	"maps"
	"slices"

	"github.com/beeper/ai-bridge/pkg/agent/harness"
)

func (cl *Client) registerProviderBuiltInToolHooks(agentHarness *harness.AgentHarness) {
	if agentHarness == nil {
		return
	}
	agentHarness.On("before_provider_payload", func(_ context.Context, event harness.AgentHarnessEvent) (any, error) {
		if event.Model == nil {
			return nil, nil
		}
		payload, changed := addBuiltInToolsToPayload(event.Payload, event.Model.BuiltInTools)
		if !changed {
			return nil, nil
		}
		return harness.BeforeProviderPayloadResult{Payload: payload}, nil
	})
}

func addBuiltInToolsToPayload(payload any, builtInTools []string) (any, bool) {
	body, ok := payload.(map[string]any)
	if !ok || len(builtInTools) == 0 {
		return payload, false
	}

	next := clonePayloadMap(body)
	tools := toolsAsAny(next["tools"])
	changed := false
	for _, toolType := range builtInTools {
		before := len(tools)
		tools = appendBuiltInTool(tools, toolType)
		changed = changed || len(tools) != before
	}
	if !changed {
		return payload, false
	}
	next["tools"] = tools
	return next, true
}

func appendBuiltInTool(tools []any, toolType string) []any {
	for _, tool := range tools {
		if toolMap, ok := tool.(map[string]any); ok {
			if toolMap["type"] == toolType || toolMap["name"] == toolType {
				return tools
			}
			if toolMap["type"] == "function" && toolMap["name"] == builtInToolFunctionName(toolType) {
				return tools
			}
		}
	}
	return append(tools, map[string]any{"type": toolType})
}

func builtInToolFunctionName(toolType string) string {
	switch toolType {
	case "image_generation":
		return "image_generation"
	default:
		return toolType
	}
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
