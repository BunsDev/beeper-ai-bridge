package utils

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	ai "github.com/beeper/ai-bridge/pkg/ai"
)

var schemaValidatorCache sync.Map

func ValidateToolCall(tools []ai.Tool, toolCall ai.ToolCall) (map[string]any, error) {
	for _, tool := range tools {
		if tool.Name == toolCall.Name {
			return ValidateToolArguments(tool, toolCall)
		}
	}
	return nil, fmt.Errorf("Tool %q not found", toolCall.Name)
}

func ValidateToolArguments(tool ai.Tool, toolCall ai.ToolCall) (map[string]any, error) {
	args := cloneArgs(toolCall.Arguments)
	if len(tool.Parameters) == 0 {
		return args, nil
	}
	coerceWithJSONSchema(args, tool.Parameters)
	errors := validateToolJSONSchema(args, tool.Parameters)
	if len(errors) == 0 {
		return args, nil
	}
	rawArgs, _ := json.MarshalIndent(toolCall.Arguments, "", "  ")
	return nil, fmt.Errorf("Validation failed for tool %q:\n%s\n\nReceived arguments:\n%s", toolCall.Name, strings.Join(errors, "\n"), string(rawArgs))
}

func validateToolJSONSchema(value any, schema map[string]any) []string {
	validator, err := getJSONSchemaValidator(schema)
	if err != nil {
		return []string{fmt.Sprintf("  - root: %s", err.Error())}
	}
	err = validator.Validate(value)
	if err == nil {
		return nil
	}
	var validationErr *jsonschema.ValidationError
	if !errors.As(err, &validationErr) {
		return []string{fmt.Sprintf("  - root: %s", err.Error())}
	}
	out := validationErr.BasicOutput()
	if out == nil {
		return []string{"  - root: Unknown validation error"}
	}
	formatted := formatJSONSchemaOutputErrors(out)
	if len(formatted) == 0 {
		return []string{"  - root: Unknown validation error"}
	}
	return formatted
}

func getJSONSchemaValidator(schema map[string]any) (*jsonschema.Schema, error) {
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	key := string(raw)
	if cached, ok := schemaValidatorCache.Load(key); ok {
		return cached.(*jsonschema.Schema), nil
	}
	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft7)
	const schemaLocation = "schema.json"
	if err := compiler.AddResource(schemaLocation, normalized); err != nil {
		return nil, err
	}
	validator, err := compiler.Compile(schemaLocation)
	if err != nil {
		return nil, err
	}
	actual, _ := schemaValidatorCache.LoadOrStore(key, validator)
	return actual.(*jsonschema.Schema), nil
}

func formatJSONSchemaOutputErrors(out *jsonschema.OutputUnit) []string {
	errors := []string{}
	var visit func(unit jsonschema.OutputUnit)
	visit = func(unit jsonschema.OutputUnit) {
		if unit.Error != nil {
			errors = append(errors, fmt.Sprintf("  - %s: %s", formatJSONPointerPath(unit.InstanceLocation), unit.Error.String()))
		}
		for _, nested := range unit.Errors {
			visit(nested)
		}
	}
	visit(*out)
	return errors
}

func formatJSONPointerPath(path string) string {
	if path == "" || path == "/" {
		return "root"
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, part := range parts {
		part = strings.ReplaceAll(part, "~1", "/")
		part = strings.ReplaceAll(part, "~0", "~")
		parts[i] = part
	}
	return strings.Join(parts, ".")
}

func cloneArgs(args map[string]any) map[string]any {
	raw, err := json.Marshal(args)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func coerceWithJSONSchema(value any, schema map[string]any) any {
	for _, nested := range schemaList(schema["allOf"]) {
		value = coerceWithJSONSchema(value, nested)
	}
	for _, nested := range schemaList(schema["anyOf"]) {
		candidate := cloneAny(value)
		coerced := coerceWithJSONSchema(candidate, nested)
		if len(validateToolJSONSchema(coerced, nested)) == 0 {
			value = coerced
			break
		}
	}
	for _, nested := range schemaList(schema["oneOf"]) {
		candidate := cloneAny(value)
		coerced := coerceWithJSONSchema(candidate, nested)
		if len(validateToolJSONSchema(coerced, nested)) == 0 {
			value = coerced
			break
		}
	}
	types := schemaTypes(schema)
	matchesUnionMember := len(types) > 1 && matchesAnyJSONType(value, types)
	if len(types) > 0 && !matchesUnionMember {
		for _, schemaType := range types {
			next := coercePrimitiveByType(value, schemaType)
			if !reflect.DeepEqual(next, value) {
				value = next
				break
			}
		}
	}
	if slices.Contains(types, "object") {
		if object, ok := value.(map[string]any); ok {
			coerceObjectProperties(object, schema)
		}
	}
	if slices.Contains(types, "array") {
		if items, ok := value.([]any); ok {
			coerceArrayItems(items, schema)
		}
	}
	return value
}

func coerceObjectProperties(value map[string]any, schema map[string]any) {
	properties, _ := schema["properties"].(map[string]any)
	defined := map[string]bool{}
	for key, rawSchema := range properties {
		propertySchema, ok := rawSchema.(map[string]any)
		if !ok {
			continue
		}
		defined[key] = true
		if current, ok := value[key]; ok {
			value[key] = coerceWithJSONSchema(current, propertySchema)
		}
	}
	if additionalSchema, ok := schema["additionalProperties"].(map[string]any); ok {
		for key, current := range value {
			if !defined[key] {
				value[key] = coerceWithJSONSchema(current, additionalSchema)
			}
		}
	}
}

func coerceArrayItems(value []any, schema map[string]any) {
	if itemSchemas, ok := schema["items"].([]any); ok {
		for i := range value {
			if i >= len(itemSchemas) {
				continue
			}
			if itemSchema, ok := itemSchemas[i].(map[string]any); ok {
				value[i] = coerceWithJSONSchema(value[i], itemSchema)
			}
		}
		return
	}
	if itemSchema, ok := schema["items"].(map[string]any); ok {
		for i := range value {
			value[i] = coerceWithJSONSchema(value[i], itemSchema)
		}
	}
}

func schemaTypes(schema map[string]any) []string {
	switch raw := schema["type"].(type) {
	case string:
		return []string{raw}
	case []any:
		types := []string{}
		for _, item := range raw {
			if text, ok := item.(string); ok {
				types = append(types, text)
			}
		}
		return types
	default:
		return nil
	}
}

func matchesAnyJSONType(value any, types []string) bool {
	for _, schemaType := range types {
		if matchesJSONType(value, schemaType) {
			return true
		}
	}
	return false
}

func matchesJSONType(value any, schemaType string) bool {
	switch schemaType {
	case "number":
		_, ok := value.(float64)
		return ok
	case "integer":
		number, ok := value.(float64)
		return ok && math.Trunc(number) == number
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "null":
		return value == nil
	case "array":
		_, ok := value.([]any)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	default:
		return false
	}
}

func coercePrimitiveByType(value any, schemaType string) any {
	switch schemaType {
	case "number":
		if value == nil {
			return float64(0)
		}
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			if parsed, err := strconv.ParseFloat(text, 64); err == nil {
				return parsed
			}
		}
		if boolean, ok := value.(bool); ok {
			if boolean {
				return float64(1)
			}
			return float64(0)
		}
	case "integer":
		if value == nil {
			return float64(0)
		}
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			if parsed, err := strconv.ParseFloat(text, 64); err == nil && math.Trunc(parsed) == parsed {
				return parsed
			}
		}
		if boolean, ok := value.(bool); ok {
			if boolean {
				return float64(1)
			}
			return float64(0)
		}
	case "boolean":
		if value == nil {
			return false
		}
		if text, ok := value.(string); ok {
			if text == "true" {
				return true
			}
			if text == "false" {
				return false
			}
		}
		if number, ok := value.(float64); ok {
			if number == 1 {
				return true
			}
			if number == 0 {
				return false
			}
		}
	case "string":
		if value == nil {
			return ""
		}
		switch v := value.(type) {
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		case bool:
			return strconv.FormatBool(v)
		}
	case "null":
		if value == "" || value == float64(0) || value == false {
			return nil
		}
	}
	return value
}

func cloneAny(value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out any
	if json.Unmarshal(raw, &out) != nil {
		return value
	}
	return out
}

func schemaList(value any) []map[string]any {
	rawList, ok := value.([]any)
	if !ok {
		return nil
	}
	out := []map[string]any{}
	for _, item := range rawList {
		if schema, ok := item.(map[string]any); ok {
			out = append(out, schema)
		}
	}
	return out
}
