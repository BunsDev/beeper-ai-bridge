package harness

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type PromptTemplateDiagnostic struct {
	Type    string
	Code    string
	Message string
	Path    string
}

type LoadPromptTemplatesResult struct {
	PromptTemplates []PromptTemplate
	Diagnostics     []PromptTemplateDiagnostic
}

func LoadPromptTemplates(paths ...string) LoadPromptTemplatesResult {
	result := LoadPromptTemplatesResult{}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			if !os.IsNotExist(err) {
				result.Diagnostics = append(result.Diagnostics, PromptTemplateDiagnostic{Type: "warning", Code: "file_info_failed", Message: err.Error(), Path: path})
			}
			continue
		}
		if info.IsDir() {
			dirResult := loadPromptTemplatesFromDir(path)
			result.PromptTemplates = append(result.PromptTemplates, dirResult.PromptTemplates...)
			result.Diagnostics = append(result.Diagnostics, dirResult.Diagnostics...)
		} else if strings.HasSuffix(info.Name(), ".md") {
			template, diagnostics := loadPromptTemplateFromFile(path)
			if template != nil {
				result.PromptTemplates = append(result.PromptTemplates, *template)
			}
			result.Diagnostics = append(result.Diagnostics, diagnostics...)
		}
	}
	return result
}

func loadPromptTemplatesFromDir(dir string) LoadPromptTemplatesResult {
	result := LoadPromptTemplatesResult{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, PromptTemplateDiagnostic{Type: "warning", Code: "list_failed", Message: err.Error(), Path: dir})
		return result
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		template, diagnostics := loadPromptTemplateFromFile(path)
		if template != nil {
			result.PromptTemplates = append(result.PromptTemplates, *template)
		}
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
	}
	return result
}

func loadPromptTemplateFromFile(path string) (*PromptTemplate, []PromptTemplateDiagnostic) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, []PromptTemplateDiagnostic{{Type: "warning", Code: "read_failed", Message: err.Error(), Path: path}}
	}
	frontmatter, body := parseFrontmatter(string(raw))
	description := frontmatter["description"]
	if description == "" {
		for _, line := range strings.Split(body, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			description = firstRunes(trimmed, 60)
			if len([]rune(trimmed)) > 60 {
				description += "..."
			}
			break
		}
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return &PromptTemplate{Name: name, Description: description, Content: body}, nil
}

func ParseCommandArgs(argsString string) []string {
	args := []string{}
	current := strings.Builder{}
	var inQuote rune
	for _, char := range argsString {
		if inQuote != 0 {
			if char == inQuote {
				inQuote = 0
			} else {
				current.WriteRune(char)
			}
			continue
		}
		switch char {
		case '"', '\'':
			inQuote = char
		case ' ', '\t':
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(char)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

func SubstituteArgs(content string, args []string) string {
	result := replaceNumberedArgs(content, args)
	result = replaceSliceArgs(result, args)
	allArgs := strings.Join(args, " ")
	result = strings.ReplaceAll(result, "$ARGUMENTS", allArgs)
	result = strings.ReplaceAll(result, "$@", allArgs)
	return result
}

func FormatPromptTemplateInvocation(template PromptTemplate, args []string) string {
	return SubstituteArgs(template.Content, args)
}

func replaceNumberedArgs(content string, args []string) string {
	var out strings.Builder
	for i := 0; i < len(content); i++ {
		if content[i] != '$' || i+1 >= len(content) || content[i+1] < '0' || content[i+1] > '9' {
			out.WriteByte(content[i])
			continue
		}
		j := i + 1
		for j < len(content) && content[j] >= '0' && content[j] <= '9' {
			j++
		}
		index, _ := strconv.Atoi(content[i+1 : j])
		if index > 0 && index <= len(args) {
			out.WriteString(args[index-1])
		}
		i = j - 1
	}
	return out.String()
}

func replaceSliceArgs(content string, args []string) string {
	var out strings.Builder
	for i := 0; i < len(content); i++ {
		if !strings.HasPrefix(content[i:], "${@:") {
			out.WriteByte(content[i])
			continue
		}
		end := strings.IndexByte(content[i:], '}')
		if end == -1 {
			out.WriteByte(content[i])
			continue
		}
		expr := content[i+4 : i+end]
		parts := strings.Split(expr, ":")
		start, _ := strconv.Atoi(parts[0])
		start--
		if start < 0 {
			start = 0
		}
		stop := len(args)
		if len(parts) > 1 {
			length, _ := strconv.Atoi(parts[1])
			stop = start + length
			if stop > len(args) {
				stop = len(args)
			}
		}
		if start < len(args) {
			out.WriteString(strings.Join(args[start:stop], " "))
		}
		i += end
	}
	return out.String()
}

func parseFrontmatter(content string) (map[string]string, string) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n")
	if !strings.HasPrefix(normalized, "---") {
		return map[string]string{}, normalized
	}
	end := strings.Index(normalized[3:], "\n---")
	if end == -1 {
		return map[string]string{}, normalized
	}
	yamlText := normalized[4 : 3+end]
	body := strings.TrimSpace(normalized[3+end+4:])
	values := map[string]string{}
	for _, line := range strings.Split(yamlText, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return values, body
}

func firstRunes(value string, count int) string {
	runes := []rune(value)
	if len(runes) <= count {
		return value
	}
	return string(runes[:count])
}
