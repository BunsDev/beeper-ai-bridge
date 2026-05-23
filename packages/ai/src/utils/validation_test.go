package utils

import (
	"strings"
	"testing"

	ai "github.com/earendil-works/pi-mono/packages/ai/src"
)

func TestValidateToolArgumentsCoercesJSONSchemaValues(t *testing.T) {
	tool := ai.Tool{Name: "calc", Parameters: map[string]any{
		"type": "object",
		"required": []any{"count", "enabled"},
		"properties": map[string]any{
			"count":   map[string]any{"type": "integer"},
			"enabled": map[string]any{"type": "boolean"},
			"label":   map[string]any{"type": "string"},
		},
	}}
	args, err := ValidateToolArguments(tool, ai.ToolCall{Name: "calc", Arguments: map[string]any{"count": "3", "enabled": "true", "label": 12}})
	if err != nil {
		t.Fatal(err)
	}
	if args["count"] != float64(3) || args["enabled"] != true || args["label"] != "12" {
		t.Fatalf("unexpected coerced args %#v", args)
	}
}

func TestValidateToolArgumentsReportsMissingRequired(t *testing.T) {
	tool := ai.Tool{Name: "read", Parameters: map[string]any{
		"type":       "object",
		"required":   []any{"path"},
		"properties": map[string]any{"path": map[string]any{"type": "string"}},
	}}
	_, err := ValidateToolArguments(tool, ai.ToolCall{Name: "read", Arguments: map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), "path") || !strings.Contains(err.Error(), "Received arguments") {
		t.Fatalf("expected validation error, got %v", err)
	}
}
