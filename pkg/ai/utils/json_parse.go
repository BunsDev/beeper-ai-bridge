package utils

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var validUnicodeEscape = regexp.MustCompile(`^[0-9a-fA-F]{4}$`)

func RepairJSON(input string) string {
	var repaired strings.Builder
	inString := false
	for index := 0; index < len(input); index++ {
		char := input[index]
		if !inString {
			repaired.WriteByte(char)
			if char == '"' {
				inString = true
			}
			continue
		}
		if char == '"' {
			repaired.WriteByte(char)
			inString = false
			continue
		}
		if char == '\\' {
			if index+1 >= len(input) {
				repaired.WriteString(`\\`)
				continue
			}
			nextChar := input[index+1]
			if nextChar == 'u' && index+5 < len(input) {
				unicodeDigits := input[index+2 : index+6]
				if validUnicodeEscape.MatchString(unicodeDigits) {
					repaired.WriteString(`\u`)
					repaired.WriteString(unicodeDigits)
					index += 5
					continue
				}
			}
			if isValidJSONEscape(nextChar) {
				repaired.WriteByte('\\')
				repaired.WriteByte(nextChar)
				index++
				continue
			}
			repaired.WriteString(`\\`)
			continue
		}
		if isControlCharacter(char) {
			repaired.WriteString(escapeControlCharacter(char))
			continue
		}
		repaired.WriteByte(char)
	}
	return repaired.String()
}

func ParseJSONWithRepair[T any](input string) (T, error) {
	var parsed T
	if err := json.Unmarshal([]byte(input), &parsed); err == nil {
		return parsed, nil
	} else {
		repaired := RepairJSON(input)
		if repaired != input {
			if repairedErr := json.Unmarshal([]byte(repaired), &parsed); repairedErr == nil {
				return parsed, nil
			}
		}
		return parsed, err
	}
}

func ParseStreamingJSON(partial string) map[string]any {
	partial = strings.TrimSpace(partial)
	if partial == "" {
		return map[string]any{}
	}
	if parsed, err := ParseJSONWithRepair[map[string]any](partial); err == nil {
		return parsed
	}
	return parseCompletedObjectMembers(RepairJSON(partial))
}

func parseCompletedObjectMembers(partial string) map[string]any {
	out := map[string]any{}
	text := strings.TrimSpace(partial)
	if !strings.HasPrefix(text, "{") {
		return out
	}
	text = strings.TrimPrefix(text, "{")
	inString := false
	escaped := false
	depth := 0
	start := 0
	for index, char := range text {
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' && inString {
			escaped = true
			continue
		}
		if char == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if char == '{' || char == '[' {
			depth++
			continue
		}
		if char == '}' || char == ']' {
			if depth > 0 {
				depth--
				continue
			}
		}
		if depth > 0 {
			continue
		}
		if char == ',' || char == '}' {
			parseObjectMember(out, text[start:index])
			start = index + len(string(char))
			if char == '}' {
				break
			}
		}
	}
	return out
}

func parseObjectMember(out map[string]any, member string) {
	member = strings.TrimSpace(member)
	if member == "" {
		return
	}
	var parsed map[string]any
	if json.Unmarshal([]byte("{"+member+"}"), &parsed) == nil {
		for key, value := range parsed {
			out[key] = value
		}
	}
}

func isValidJSONEscape(char byte) bool {
	switch char {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't', 'u':
		return true
	default:
		return false
	}
}

func isControlCharacter(char byte) bool {
	return char <= 0x1f
}

func escapeControlCharacter(char byte) string {
	switch char {
	case '\b':
		return `\b`
	case '\f':
		return `\f`
	case '\n':
		return `\n`
	case '\r':
		return `\r`
	case '\t':
		return `\t`
	default:
		return fmt.Sprintf(`\u%04x`, char)
	}
}
