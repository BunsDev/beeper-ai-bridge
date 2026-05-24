package harness

import (
	"fmt"
	"sort"
	"strings"

	gitignore "github.com/sabhiram/go-gitignore"
	"gopkg.in/yaml.v3"
)

const maxSkillNameLength = 64
const maxSkillDescriptionLength = 1024

var skillIgnoreFileNames = []string{".gitignore", ".ignore", ".fdignore"}

type SkillDiagnostic struct {
	Type    string
	Code    string
	Message string
	Path    string
}

type LoadSkillsResult struct {
	Skills      []Skill
	Diagnostics []SkillDiagnostic
}

type SkillFileSystem interface {
	TryFileInfo(path string) Result[FileInfo, *FileError]
	TryListDir(path string) Result[[]FileInfo, *FileError]
	TryReadTextFile(path string) Result[string, *FileError]
	TryCanonicalPath(path string) Result[string, *FileError]
}

type SourcedSkill[TSource any, TSkill any] struct {
	Skill  TSkill
	Source TSource
}

type SourcedSkillDiagnostic[TSource any] struct {
	SkillDiagnostic
	Source TSource
}

type SourcedSkillInput[TSource any] struct {
	Path   string
	Source TSource
}

type LoadSourcedSkillsResult[TSource any, TSkill any] struct {
	Skills      []SourcedSkill[TSource, TSkill]
	Diagnostics []SourcedSkillDiagnostic[TSource]
}

func FormatSkillInvocation(skill Skill, additionalInstructions string) string {
	block := fmt.Sprintf("<skill name=\"%s\" location=\"%s\">\nReferences are relative to %s.\n\n%s\n</skill>", skill.Name, skill.FilePath, dirnameEnvPath(skill.FilePath), skill.Content)
	if additionalInstructions != "" {
		return block + "\n\n" + additionalInstructions
	}
	return block
}

func LoadSkills(env SkillFileSystem, dirs ...string) LoadSkillsResult {
	result := LoadSkillsResult{}
	for _, dir := range dirs {
		rootInfo := env.TryFileInfo(dir)
		if !rootInfo.OK {
			if rootInfo.Error.Code != FileErrorNotFound {
				result.Diagnostics = append(result.Diagnostics, SkillDiagnostic{Type: "warning", Code: "file_info_failed", Message: rootInfo.Error.Error(), Path: dir})
			}
			continue
		}
		kind, ok := resolveSkillKind(env, rootInfo.Value, &result.Diagnostics)
		if !ok || kind != FileKindDirectory {
			continue
		}
		loaded := loadSkillsFromDirInternal(env, rootInfo.Value.Path, true, newSkillIgnoreMatcher(), rootInfo.Value.Path)
		result.Skills = append(result.Skills, loaded.Skills...)
		result.Diagnostics = append(result.Diagnostics, loaded.Diagnostics...)
	}
	return result
}

func LoadLocalSkills(dirs ...string) LoadSkillsResult {
	return LoadSkills(NewLocalExecutionEnv(""), dirs...)
}

func LoadSourcedSkills[TSource any](
	env SkillFileSystem,
	inputs []SourcedSkillInput[TSource],
) LoadSourcedSkillsResult[TSource, Skill] {
	return LoadMappedSourcedSkills(env, inputs, func(skill Skill, _ TSource) Skill {
		return skill
	})
}

func LoadMappedSourcedSkills[TSource any, TSkill any](
	env SkillFileSystem,
	inputs []SourcedSkillInput[TSource],
	mapSkill func(Skill, TSource) TSkill,
) LoadSourcedSkillsResult[TSource, TSkill] {
	result := LoadSourcedSkillsResult[TSource, TSkill]{}
	for _, input := range inputs {
		loaded := LoadSkills(env, input.Path)
		for _, skill := range loaded.Skills {
			result.Skills = append(result.Skills, SourcedSkill[TSource, TSkill]{
				Skill:  mapSkill(skill, input.Source),
				Source: input.Source,
			})
		}
		for _, diagnostic := range loaded.Diagnostics {
			result.Diagnostics = append(result.Diagnostics, SourcedSkillDiagnostic[TSource]{
				SkillDiagnostic: diagnostic,
				Source:          input.Source,
			})
		}
	}
	return result
}

func loadSkillsFromDirInternal(env SkillFileSystem, dir string, includeRootFiles bool, ignoreMatcher *skillIgnoreMatcher, rootDir string) LoadSkillsResult {
	result := LoadSkillsResult{}
	dirInfo := env.TryFileInfo(dir)
	if !dirInfo.OK {
		if dirInfo.Error.Code != FileErrorNotFound {
			result.Diagnostics = append(result.Diagnostics, SkillDiagnostic{Type: "warning", Code: "file_info_failed", Message: dirInfo.Error.Error(), Path: dir})
		}
		return result
	}
	kind, ok := resolveSkillKind(env, dirInfo.Value, &result.Diagnostics)
	if !ok || kind != FileKindDirectory {
		return result
	}

	addSkillIgnoreRules(env, ignoreMatcher, dir, rootDir, &result.Diagnostics)

	entriesResult := env.TryListDir(dir)
	if !entriesResult.OK {
		result.Diagnostics = append(result.Diagnostics, SkillDiagnostic{Type: "warning", Code: "list_failed", Message: entriesResult.Error.Error(), Path: dir})
		return result
	}
	entries := entriesResult.Value

	for _, entry := range entries {
		if entry.Name != "SKILL.md" {
			continue
		}
		kind, ok := resolveSkillKind(env, entry, &result.Diagnostics)
		if !ok || kind != FileKindFile {
			continue
		}
		relPath := relativeEnvPath(rootDir, entry.Path)
		if ignoreMatcher.ignores(relPath) {
			continue
		}
		skill, diagnostics := loadSkillFromFile(env, entry.Path)
		if skill != nil {
			result.Skills = append(result.Skills, *skill)
		}
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
		return result
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name, ".") || entry.Name == "node_modules" {
			continue
		}
		kind, ok := resolveSkillKind(env, entry, &result.Diagnostics)
		if !ok {
			continue
		}
		relPath := relativeEnvPath(rootDir, entry.Path)
		ignorePath := relPath
		if kind == FileKindDirectory {
			ignorePath += "/"
		}
		if ignoreMatcher.ignores(ignorePath) {
			continue
		}
		if kind == FileKindDirectory {
			loaded := loadSkillsFromDirInternal(env, entry.Path, false, ignoreMatcher, rootDir)
			result.Skills = append(result.Skills, loaded.Skills...)
			result.Diagnostics = append(result.Diagnostics, loaded.Diagnostics...)
			continue
		}
		if kind != FileKindFile || !includeRootFiles || !strings.HasSuffix(entry.Name, ".md") {
			continue
		}
		skill, diagnostics := loadSkillFromFile(env, entry.Path)
		if skill != nil {
			result.Skills = append(result.Skills, *skill)
		}
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
	}
	return result
}

func addSkillIgnoreRules(env SkillFileSystem, ignoreMatcher *skillIgnoreMatcher, dir string, rootDir string, diagnostics *[]SkillDiagnostic) {
	relativeDir := relativeEnvPath(rootDir, dir)
	prefix := ""
	if relativeDir != "" {
		prefix = relativeDir + "/"
	}
	for _, filename := range skillIgnoreFileNames {
		ignorePath := joinEnvPath(dir, filename)
		info := env.TryFileInfo(ignorePath)
		if !info.OK {
			if info.Error.Code != FileErrorNotFound {
				*diagnostics = append(*diagnostics, SkillDiagnostic{Type: "warning", Code: "file_info_failed", Message: info.Error.Error(), Path: ignorePath})
			}
			continue
		}
		if info.Value.Kind != FileKindFile {
			continue
		}
		content := env.TryReadTextFile(ignorePath)
		if !content.OK {
			*diagnostics = append(*diagnostics, SkillDiagnostic{Type: "warning", Code: "read_failed", Message: content.Error.Error(), Path: ignorePath})
			continue
		}
		patterns := []string{}
		for _, line := range strings.Split(strings.ReplaceAll(content.Value, "\r\n", "\n"), "\n") {
			pattern := prefixIgnorePattern(line, prefix)
			if pattern != "" {
				patterns = append(patterns, pattern)
			}
		}
		ignoreMatcher.add(patterns...)
	}
}

func prefixIgnorePattern(line string, prefix string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "\\#") {
		return ""
	}
	pattern := line
	negated := false
	if strings.HasPrefix(pattern, "!") {
		negated = true
		pattern = pattern[1:]
	} else if strings.HasPrefix(pattern, "\\!") {
		pattern = pattern[1:]
	}
	pattern = strings.TrimPrefix(pattern, "/")
	if prefix != "" {
		pattern = prefix + pattern
	}
	if negated {
		return "!" + pattern
	}
	return pattern
}

func loadSkillFromFile(env SkillFileSystem, path string) (*Skill, []SkillDiagnostic) {
	raw := env.TryReadTextFile(path)
	if !raw.OK {
		return nil, []SkillDiagnostic{{Type: "warning", Code: "read_failed", Message: raw.Error.Error(), Path: path}}
	}
	frontmatter, body, err := parseSkillFrontmatter(raw.Value)
	if err != nil {
		return nil, []SkillDiagnostic{{Type: "warning", Code: "parse_failed", Message: err.Error(), Path: path}}
	}
	description := frontmatter.Description
	diagnostics := []SkillDiagnostic{}
	for _, message := range validateSkillDescription(description) {
		diagnostics = append(diagnostics, SkillDiagnostic{Type: "warning", Code: "invalid_metadata", Message: message, Path: path})
	}
	name := frontmatter.Name
	parent := basenameEnvPath(dirnameEnvPath(path))
	if name == "" {
		name = parent
	}
	for _, message := range validateSkillName(name, parent) {
		diagnostics = append(diagnostics, SkillDiagnostic{Type: "warning", Code: "invalid_metadata", Message: message, Path: path})
	}
	if strings.TrimSpace(description) == "" {
		return nil, diagnostics
	}
	return &Skill{
		Name:                   name,
		Description:            description,
		Content:                body,
		FilePath:               path,
		DisableModelInvocation: frontmatter.DisableModelInvocation,
	}, diagnostics
}

func resolveSkillKind(env SkillFileSystem, info FileInfo, diagnostics *[]SkillDiagnostic) (FileKind, bool) {
	if info.Kind == FileKindFile || info.Kind == FileKindDirectory {
		return info.Kind, true
	}
	canonicalPath := env.TryCanonicalPath(info.Path)
	if !canonicalPath.OK {
		if canonicalPath.Error.Code != FileErrorNotFound {
			*diagnostics = append(*diagnostics, SkillDiagnostic{Type: "warning", Code: "file_info_failed", Message: canonicalPath.Error.Error(), Path: info.Path})
		}
		return "", false
	}
	target := env.TryFileInfo(canonicalPath.Value)
	if !target.OK {
		if target.Error.Code != FileErrorNotFound {
			*diagnostics = append(*diagnostics, SkillDiagnostic{Type: "warning", Code: "file_info_failed", Message: target.Error.Error(), Path: info.Path})
		}
		return "", false
	}
	if target.Value.Kind == FileKindFile || target.Value.Kind == FileKindDirectory {
		return target.Value.Kind, true
	}
	return "", false
}

type skillFrontmatter struct {
	Name                   string `yaml:"name"`
	Description            string `yaml:"description"`
	DisableModelInvocation bool   `yaml:"disable-model-invocation"`
}

func parseSkillFrontmatter(content string) (skillFrontmatter, string, error) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n")
	if !strings.HasPrefix(normalized, "---") {
		return skillFrontmatter{}, normalized, nil
	}
	end := strings.Index(normalized[3:], "\n---")
	if end == -1 {
		return skillFrontmatter{}, normalized, nil
	}
	yamlText := normalized[4 : 3+end]
	body := strings.TrimSpace(normalized[3+end+4:])
	var frontmatter skillFrontmatter
	if err := yaml.Unmarshal([]byte(yamlText), &frontmatter); err != nil {
		return skillFrontmatter{}, "", err
	}
	return frontmatter, body, nil
}

func validateSkillName(name string, parent string) []string {
	errors := []string{}
	if name != parent {
		errors = append(errors, fmt.Sprintf("name %q does not match parent directory %q", name, parent))
	}
	if len(name) > maxSkillNameLength {
		errors = append(errors, fmt.Sprintf("name exceeds %d characters (%d)", maxSkillNameLength, len(name)))
	}
	if !isSkillName(name) {
		errors = append(errors, "name contains invalid characters (must be lowercase a-z, 0-9, hyphens only)")
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		errors = append(errors, "name must not start or end with a hyphen")
	}
	if strings.Contains(name, "--") {
		errors = append(errors, "name must not contain consecutive hyphens")
	}
	return errors
}

func validateSkillDescription(description string) []string {
	if strings.TrimSpace(description) == "" {
		return []string{"description is required"}
	}
	if len(description) > maxSkillDescriptionLength {
		return []string{fmt.Sprintf("description exceeds %d characters (%d)", maxSkillDescriptionLength, len(description))}
	}
	return nil
}

func isSkillName(name string) bool {
	if name == "" {
		return false
	}
	for _, char := range name {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' {
			continue
		}
		return false
	}
	return true
}

type skillIgnoreMatcher struct {
	lines []string
	impl  *gitignore.GitIgnore
}

func newSkillIgnoreMatcher() *skillIgnoreMatcher {
	return &skillIgnoreMatcher{impl: gitignore.CompileIgnoreLines()}
}

func (m *skillIgnoreMatcher) add(lines ...string) {
	if len(lines) == 0 {
		return
	}
	m.lines = append(m.lines, lines...)
	m.impl = gitignore.CompileIgnoreLines(m.lines...)
}

func (m *skillIgnoreMatcher) ignores(path string) bool {
	return m.impl.MatchesPath(path)
}

func joinEnvPath(base string, child string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(child, "/")
}

func dirnameEnvPath(path string) string {
	normalized := strings.TrimRight(path, "/")
	slashIndex := strings.LastIndex(normalized, "/")
	if slashIndex <= 0 {
		return "/"
	}
	return normalized[:slashIndex]
}

func basenameEnvPath(path string) string {
	normalized := strings.TrimRight(path, "/")
	slashIndex := strings.LastIndex(normalized, "/")
	if slashIndex == -1 {
		return normalized
	}
	return normalized[slashIndex+1:]
}

func relativeEnvPath(root string, path string) string {
	normalizedRoot := strings.TrimRight(root, "/")
	normalizedPath := strings.TrimRight(path, "/")
	if normalizedPath == normalizedRoot {
		return ""
	}
	if strings.HasPrefix(normalizedPath, normalizedRoot+"/") {
		return normalizedPath[len(normalizedRoot)+1:]
	}
	return strings.TrimLeft(normalizedPath, "/")
}
