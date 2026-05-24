package utils

import (
	"strings"
	"testing"

	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func TestValidateToolArgumentsCoercesJSONSchemaValues(t *testing.T) {
	tool := ai.Tool{Name: "calc", Parameters: map[string]any{
		"type":     "object",
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

func TestValidateToolArgumentsUsesJSONSchemaKeywords(t *testing.T) {
	tool := ai.Tool{Name: "bounded", Parameters: map[string]any{
		"type":       "object",
		"properties": map[string]any{"count": map[string]any{"type": "integer", "minimum": 5.0}},
	}}
	_, err := ValidateToolArguments(tool, ai.ToolCall{Name: "bounded", Arguments: map[string]any{"count": 3}})
	if err == nil || !strings.Contains(err.Error(), "count") || !strings.Contains(err.Error(), "minimum") {
		t.Fatalf("expected minimum validation error, got %v", err)
	}
}

func TestValidateToolArgumentsCoercesBeforeJSONSchemaValidation(t *testing.T) {
	tool := ai.Tool{Name: "bounded", Parameters: map[string]any{
		"type":       "object",
		"properties": map[string]any{"count": map[string]any{"type": "integer", "minimum": 5.0}},
	}}
	args, err := ValidateToolArguments(tool, ai.ToolCall{Name: "bounded", Arguments: map[string]any{"count": "5"}})
	if err != nil {
		t.Fatal(err)
	}
	if args["count"] != float64(5) {
		t.Fatalf("unexpected coerced count %#v", args["count"])
	}
}
