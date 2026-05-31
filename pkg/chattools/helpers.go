package chattools

import (
	"encoding/json"
	"fmt"
	"strings"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func jsonResult(value any) (agent.AgentToolResult[any], error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return agent.AgentToolResult[any]{}, err
	}
	return agent.AgentToolResult[any]{
		Content: []ai.ContentBlock{{Type: "text", Text: string(raw)}},
		Details: value,
	}, nil
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
		"required":             required,
	}
}

func stringParam(params any, key string) (string, error) {
	values, ok := params.(map[string]any)
	if !ok {
		return "", fmt.Errorf("expected object arguments")
	}
	value, ok := values[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("missing %s", key)
	}
	return value, nil
}

func intParam(params any, key string, fallback int) int {
	values, ok := params.(map[string]any)
	if !ok {
		return fallback
	}
	switch value := values[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return fallback
	}
}

func stringValueParam(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func stringSliceParam(values map[string]any, key string) []string {
	raw, ok := values[key].([]any)
	if !ok {
		if typed, ok := values[key].([]string); ok {
			return cleanStringSlice(typed)
		}
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

func cleanStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

func mapParam(values map[string]any, key string) map[string]any {
	value, ok := values[key].(map[string]any)
	if !ok || len(value) == 0 {
		return nil
	}
	out := map[string]any{}
	for k, v := range value {
		out[k] = v
	}
	return out
}

func addString(payload map[string]any, key string, value string) {
	if strings.TrimSpace(value) != "" {
		payload[key] = strings.TrimSpace(value)
	}
}

func addStrings(payload map[string]any, key string, values []string) {
	if len(values) > 0 {
		payload[key] = values
	}
}

func addMap(payload map[string]any, key string, value map[string]any) {
	if len(value) > 0 {
		payload[key] = value
	}
}

func addAny(payload map[string]any, key string, value any) {
	if value != nil {
		payload[key] = value
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
