package utils

func StringEnum(values []string, options ...StringEnumOptions) map[string]any {
	schema := map[string]any{
		"type": "string",
		"enum": append([]string{}, values...),
	}
	if len(options) == 0 {
		return schema
	}
	option := options[0]
	if option.Description != "" {
		schema["description"] = option.Description
	}
	if option.Default != "" {
		schema["default"] = option.Default
	}
	return schema
}

type StringEnumOptions struct {
	Description string
	Default     string
}
