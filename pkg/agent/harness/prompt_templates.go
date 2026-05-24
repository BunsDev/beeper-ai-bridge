package harness

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
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

type PromptTemplateFileSystem interface {
	TryFileInfo(path string) Result[FileInfo, *FileError]
	TryListDir(path string) Result[[]FileInfo, *FileError]
	TryReadTextFile(path string) Result[string, *FileError]
	TryCanonicalPath(path string) Result[string, *FileError]
}

type SourcedPromptTemplate[TSource any, TPromptTemplate any] struct {
	PromptTemplate TPromptTemplate
	Source         TSource
}

type SourcedPromptTemplateDiagnostic[TSource any] struct {
	PromptTemplateDiagnostic
	Source TSource
}

type SourcedPromptTemplateInput[TSource any] struct {
	Path   string
	Source TSource
}

type LoadSourcedPromptTemplatesResult[TSource any, TPromptTemplate any] struct {
	PromptTemplates []SourcedPromptTemplate[TSource, TPromptTemplate]
	Diagnostics     []SourcedPromptTemplateDiagnostic[TSource]
}

func LoadPromptTemplates(env PromptTemplateFileSystem, paths ...string) LoadPromptTemplatesResult {
	result := LoadPromptTemplatesResult{}
	for _, path := range paths {
		infoResult := env.TryFileInfo(path)
		if !infoResult.OK {
			if infoResult.Error.Code != FileErrorNotFound {
				result.Diagnostics = append(result.Diagnostics, PromptTemplateDiagnostic{Type: "warning", Code: "file_info_failed", Message: infoResult.Error.Error(), Path: path})
			}
			continue
		}
		info := infoResult.Value
		kind, ok := resolvePromptTemplateKind(env, info, &result.Diagnostics)
		if !ok {
			continue
		}
		if kind == FileKindDirectory {
			dirResult := loadPromptTemplatesFromDir(env, info.Path)
			result.PromptTemplates = append(result.PromptTemplates, dirResult.PromptTemplates...)
			result.Diagnostics = append(result.Diagnostics, dirResult.Diagnostics...)
		} else if kind == FileKindFile && strings.HasSuffix(info.Name, ".md") {
			template, diagnostics := loadPromptTemplateFromFile(env, info.Path)
			if template != nil {
				result.PromptTemplates = append(result.PromptTemplates, *template)
			}
			result.Diagnostics = append(result.Diagnostics, diagnostics...)
		}
	}
	return result
}

func LoadLocalPromptTemplates(paths ...string) LoadPromptTemplatesResult {
	return LoadPromptTemplates(NewLocalExecutionEnv(""), paths...)
}

func LoadSourcedPromptTemplates[TSource any](
	env PromptTemplateFileSystem,
	inputs []SourcedPromptTemplateInput[TSource],
) LoadSourcedPromptTemplatesResult[TSource, PromptTemplate] {
	return LoadMappedSourcedPromptTemplates(env, inputs, func(template PromptTemplate, _ TSource) PromptTemplate {
		return template
	})
}

func LoadMappedSourcedPromptTemplates[TSource any, TPromptTemplate any](
	env PromptTemplateFileSystem,
	inputs []SourcedPromptTemplateInput[TSource],
	mapPromptTemplate func(PromptTemplate, TSource) TPromptTemplate,
) LoadSourcedPromptTemplatesResult[TSource, TPromptTemplate] {
	result := LoadSourcedPromptTemplatesResult[TSource, TPromptTemplate]{}
	for _, input := range inputs {
		loaded := LoadPromptTemplates(env, input.Path)
		for _, template := range loaded.PromptTemplates {
			result.PromptTemplates = append(result.PromptTemplates, SourcedPromptTemplate[TSource, TPromptTemplate]{
				PromptTemplate: mapPromptTemplate(template, input.Source),
				Source:         input.Source,
			})
		}
		for _, diagnostic := range loaded.Diagnostics {
			result.Diagnostics = append(result.Diagnostics, SourcedPromptTemplateDiagnostic[TSource]{
				PromptTemplateDiagnostic: diagnostic,
				Source:                   input.Source,
			})
		}
	}
	return result
}

func loadPromptTemplatesFromDir(env PromptTemplateFileSystem, dir string) LoadPromptTemplatesResult {
	result := LoadPromptTemplatesResult{}
	entriesResult := env.TryListDir(dir)
	if !entriesResult.OK {
		result.Diagnostics = append(result.Diagnostics, PromptTemplateDiagnostic{Type: "warning", Code: "list_failed", Message: entriesResult.Error.Error(), Path: dir})
		return result
	}
	entries := entriesResult.Value
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	for _, entry := range entries {
		kind, ok := resolvePromptTemplateKind(env, entry, &result.Diagnostics)
		if !ok || kind != FileKindFile || !strings.HasSuffix(entry.Name, ".md") {
			continue
		}
		template, diagnostics := loadPromptTemplateFromFile(env, entry.Path)
		if template != nil {
			result.PromptTemplates = append(result.PromptTemplates, *template)
		}
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
	}
	return result
}

func loadPromptTemplateFromFile(env PromptTemplateFileSystem, path string) (*PromptTemplate, []PromptTemplateDiagnostic) {
	raw := env.TryReadTextFile(path)
	if !raw.OK {
		return nil, []PromptTemplateDiagnostic{{Type: "warning", Code: "read_failed", Message: raw.Error.Error(), Path: path}}
	}
	frontmatter, body, err := parsePromptTemplateFrontmatter(raw.Value)
	if err != nil {
		return nil, []PromptTemplateDiagnostic{{Type: "warning", Code: "parse_failed", Message: err.Error(), Path: path}}
	}
	description := frontmatter["description"]
	if description == "" {
		for _, line := range strings.Split(body, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			description = firstBytes(line, 60)
			if len(line) > 60 {
				description += "..."
			}
			break
		}
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return &PromptTemplate{Name: name, Description: description, Content: body}, nil
}

func resolvePromptTemplateKind(env PromptTemplateFileSystem, info FileInfo, diagnostics *[]PromptTemplateDiagnostic) (FileKind, bool) {
	if info.Kind == FileKindFile || info.Kind == FileKindDirectory {
		return info.Kind, true
	}
	canonicalPath := env.TryCanonicalPath(info.Path)
	if !canonicalPath.OK {
		if canonicalPath.Error.Code != FileErrorNotFound {
			*diagnostics = append(*diagnostics, PromptTemplateDiagnostic{Type: "warning", Code: "file_info_failed", Message: canonicalPath.Error.Error(), Path: info.Path})
		}
		return "", false
	}
	target := env.TryFileInfo(canonicalPath.Value)
	if !target.OK {
		if target.Error.Code != FileErrorNotFound {
			*diagnostics = append(*diagnostics, PromptTemplateDiagnostic{Type: "warning", Code: "file_info_failed", Message: target.Error.Error(), Path: info.Path})
		}
		return "", false
	}
	if target.Value.Kind == FileKindFile || target.Value.Kind == FileKindDirectory {
		return target.Value.Kind, true
	}
	return "", false
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
		if len(parts) > 2 || !isDecimal(parts[0]) || (len(parts) == 2 && !isDecimal(parts[1])) {
			out.WriteString(content[i : i+end+1])
			i += end
			continue
		}
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

func isDecimal(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func parsePromptTemplateFrontmatter(content string) (map[string]string, string, error) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n")
	if !strings.HasPrefix(normalized, "---") {
		return map[string]string{}, normalized, nil
	}
	end := strings.Index(normalized[3:], "\n---")
	if end == -1 {
		return map[string]string{}, normalized, nil
	}
	yamlText := normalized[4 : 3+end]
	body := strings.TrimSpace(normalized[3+end+4:])
	var parsed any
	if err := yaml.Unmarshal([]byte(yamlText), &parsed); err != nil {
		return nil, "", err
	}
	frontmatter := map[string]string{}
	values, ok := parsed.(map[string]any)
	if !ok {
		return frontmatter, body, nil
	}
	for key, value := range values {
		switch typed := value.(type) {
		case string:
			frontmatter[key] = typed
		case bool:
			frontmatter[key] = strconv.FormatBool(typed)
		}
	}
	return frontmatter, body, nil
}

func parseFrontmatter(content string) (map[string]string, string) {
	frontmatter, body, err := parsePromptTemplateFrontmatter(content)
	if err != nil {
		return map[string]string{}, content
	}
	return frontmatter, body
}

func firstBytes(value string, count int) string {
	runes := []rune(value)
	if len(runes) <= count {
		return value
	}
	return string(runes[:count])
}
