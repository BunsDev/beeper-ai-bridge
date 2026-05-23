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
	result := LoadSkills(root)
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
	result := LoadSkills(root)
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
