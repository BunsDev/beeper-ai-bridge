package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSkillsAndFormatInvocation(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "my-skill")
	if err := os.Mkdir(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("---\nname: my-skill\ndescription: Use for tests\n---\nDo the thing."), 0o600); err != nil {
		t.Fatal(err)
	}
	env := NewLocalExecutionEnv("")
	result := LoadSkills(env, root)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics %#v", result.Diagnostics)
	}
	if len(result.Skills) != 1 {
		t.Fatalf("expected one skill, got %#v", result.Skills)
	}
	skill := result.Skills[0]
	if skill.Name != "my-skill" || skill.Description != "Use for tests" || skill.Content != "Do the thing." {
		t.Fatalf("unexpected skill %#v", skill)
	}
	invocation := FormatSkillInvocation(skill, "Extra")
	for _, want := range []string{`<skill name="my-skill" location="` + skillPath + `">`, "References are relative to " + skillDir + ".", "Do the thing.", "</skill>\n\nExtra"} {
		if !strings.Contains(invocation, want) {
			t.Fatalf("expected %q in %q", want, invocation)
		}
	}
}

func TestLoadSkillsReportsInvalidMetadataAndSystemPromptEscapes(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "valid-skill")
	if err := os.Mkdir(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: Other\ndescription: Use <quoted> & \"special\" things\n---\nBody"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := LoadSkills(NewLocalExecutionEnv(""), root)
	if len(result.Skills) != 1 {
		t.Fatalf("expected skill despite diagnostics, got %#v", result.Skills)
	}
	if len(result.Diagnostics) == 0 {
		t.Fatalf("expected invalid metadata diagnostics")
	}
	prompt := FormatSkillsForSystemPrompt([]Skill{result.Skills[0], {Name: "hidden", Description: "hide", FilePath: "/tmp/h", DisableModelInvocation: true}})
	if strings.Contains(prompt, "hidden") {
		t.Fatalf("hidden skill leaked into prompt: %q", prompt)
	}
	if !strings.Contains(prompt, "Use &lt;quoted&gt; &amp; &quot;special&quot; things") {
		t.Fatalf("expected escaped prompt, got %q", prompt)
	}
}

func TestLoadSkillsHonorsIgnoreFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ignoredDir := filepath.Join(root, "ignored")
	if err := os.Mkdir(ignoredDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ignoredDir, "SKILL.md"), []byte("---\nname: ignored\ndescription: Ignore me\n---\nBody"), 0o600); err != nil {
		t.Fatal(err)
	}
	keptDir := filepath.Join(root, "kept")
	if err := os.Mkdir(keptDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keptDir, "SKILL.md"), []byte("---\nname: kept\ndescription: Keep me\n---\nBody"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := LoadSkills(NewLocalExecutionEnv(""), root)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics %#v", result.Diagnostics)
	}
	if len(result.Skills) != 1 || result.Skills[0].Name != "kept" {
		t.Fatalf("expected only kept skill, got %#v", result.Skills)
	}
}

func TestLoadSkillsReportsParseFailed(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "bad-skill")
	if err := os.Mkdir(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\ndescription: [\n---\nBody"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := LoadSkills(NewLocalExecutionEnv(""), root)
	if len(result.Skills) != 0 {
		t.Fatalf("expected no skills, got %#v", result.Skills)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "parse_failed" {
		t.Fatalf("expected parse_failed diagnostic, got %#v", result.Diagnostics)
	}
}

func TestLoadSourcedSkills(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "local-skill")
	if err := os.Mkdir(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: local-skill\ndescription: Local skill\n---\nBody"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := LoadSourcedSkills(NewLocalExecutionEnv(""), []SourcedSkillInput[string]{{Path: root, Source: "local"}})
	if len(result.Skills) != 1 {
		t.Fatalf("expected sourced skill, got %#v", result.Skills)
	}
	if result.Skills[0].Source != "local" || result.Skills[0].Skill.Name != "local-skill" {
		t.Fatalf("unexpected sourced skill %#v", result.Skills[0])
	}
}
