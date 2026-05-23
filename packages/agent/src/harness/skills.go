package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxSkillNameLength = 64
const maxSkillDescriptionLength = 1024

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

func FormatSkillInvocation(skill Skill, additionalInstructions string) string {
	block := fmt.Sprintf("<skill name=\"%s\" location=\"%s\">\nReferences are relative to %s.\n\n%s\n</skill>", skill.Name, skill.FilePath, filepath.Dir(skill.FilePath), skill.Content)
	if additionalInstructions != "" {
		return block + "\n\n" + additionalInstructions
	}
	return block
}

func LoadSkills(dirs ...string) LoadSkillsResult {
	result := LoadSkillsResult{}
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				result.Diagnostics = append(result.Diagnostics, SkillDiagnostic{Type: "warning", Code: "file_info_failed", Message: err.Error(), Path: dir})
			}
			continue
		}
		if !info.IsDir() {
			continue
		}
		loaded := loadSkillsFromDir(dir, true)
		result.Skills = append(result.Skills, loaded.Skills...)
		result.Diagnostics = append(result.Diagnostics, loaded.Diagnostics...)
	}
	return result
}

func loadSkillsFromDir(dir string, includeRootFiles bool) LoadSkillsResult {
	result := LoadSkillsResult{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, SkillDiagnostic{Type: "warning", Code: "list_failed", Message: err.Error(), Path: dir})
		return result
	}
	for _, entry := range entries {
		if entry.Name() != "SKILL.md" {
			continue
		}
		skill, diagnostics := loadSkillFromFile(filepath.Join(dir, entry.Name()))
		if skill != nil {
			result.Skills = append(result.Skills, *skill)
		}
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
		return result
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			loaded := loadSkillsFromDir(path, false)
			result.Skills = append(result.Skills, loaded.Skills...)
			result.Diagnostics = append(result.Diagnostics, loaded.Diagnostics...)
			continue
		}
		if includeRootFiles && strings.HasSuffix(entry.Name(), ".md") {
			skill, diagnostics := loadSkillFromFile(path)
			if skill != nil {
				result.Skills = append(result.Skills, *skill)
			}
			result.Diagnostics = append(result.Diagnostics, diagnostics...)
		}
	}
	return result
}

func loadSkillFromFile(path string) (*Skill, []SkillDiagnostic) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, []SkillDiagnostic{{Type: "warning", Code: "read_failed", Message: err.Error(), Path: path}}
	}
	frontmatter, body := parseFrontmatter(string(raw))
	description := frontmatter["description"]
	diagnostics := []SkillDiagnostic{}
	for _, message := range validateSkillDescription(description) {
		diagnostics = append(diagnostics, SkillDiagnostic{Type: "warning", Code: "invalid_metadata", Message: message, Path: path})
	}
	name := frontmatter["name"]
	parent := filepath.Base(filepath.Dir(path))
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
		DisableModelInvocation: frontmatter["disable-model-invocation"] == "true",
	}, diagnostics
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
