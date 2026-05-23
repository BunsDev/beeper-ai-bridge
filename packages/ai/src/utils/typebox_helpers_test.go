package utils

import "testing"

func TestStringEnumBuildsSchema(t *testing.T) {
	values := []string{"add", "subtract"}
	schema := StringEnum(values, StringEnumOptions{Description: "operation", Default: "add"})
	values[0] = "mutated"
	enumValues, ok := schema["enum"].([]string)
	if !ok {
		t.Fatalf("expected enum values, got %#v", schema["enum"])
	}
	if schema["type"] != "string" || enumValues[0] != "add" || schema["description"] != "operation" || schema["default"] != "add" {
		t.Fatalf("unexpected schema %#v", schema)
	}
}
