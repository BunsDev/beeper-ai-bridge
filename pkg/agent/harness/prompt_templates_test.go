package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCommandArgsAndSubstituteArgs(t *testing.T) {
	args := ParseCommandArgs(`one "two words" 'three words'`)
	if len(args) != 3 || args[0] != "one" || args[1] != "two words" || args[2] != "three words" {
		t.Fatalf("unexpected args %#v", args)
	}
	got := SubstituteArgs("$1 $2 $@ $ARGUMENTS ${@:2} ${@:2:1} $9", []string{"a", "b", "c"})
	want := "a b a b c a b c b c b "
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	got = SubstituteArgs("${@:x} ${@:2:x} ${@:1:2:3} ${@:0:1}", []string{"a", "b", "c"})
	want = "${@:x} ${@:2:x} ${@:1:2:3} a"
	if got != want {
		t.Fatalf("expected malformed slice placeholders to match TS behavior: %q, got %q", want, got)
	}
}

func TestLoadPromptTemplates(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte("First line is a generated description that is definitely longer than sixty characters.\n\nBody"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("---\ndescription: Listed template\n---\nRun $1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skip.txt"), []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := NewLocalExecutionEnv("")
	result := LoadPromptTemplates(env, dir)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics %#v", result.Diagnostics)
	}
	if len(result.PromptTemplates) != 2 {
		t.Fatalf("expected two templates, got %#v", result.PromptTemplates)
	}
	if result.PromptTemplates[0].Name != "a" || result.PromptTemplates[0].Description != "Listed template" {
		t.Fatalf("unexpected first template %#v", result.PromptTemplates[0])
	}
	if result.PromptTemplates[1].Name != "b" || result.PromptTemplates[1].Description != "First line is a generated description that is definitely lon..." {
		t.Fatalf("unexpected second template %#v", result.PromptTemplates[1])
	}
	if FormatPromptTemplateInvocation(result.PromptTemplates[0], []string{"check"}) != "Run check" {
		t.Fatalf("unexpected invocation")
	}
}

func TestLoadPromptTemplatesScalarFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scalar.md")
	if err := os.WriteFile(path, []byte("---\njust text\n---\nBody"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := LoadPromptTemplates(NewLocalExecutionEnv(""), path)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics %#v", result.Diagnostics)
	}
	if len(result.PromptTemplates) != 1 || result.PromptTemplates[0].Description != "Body" {
		t.Fatalf("unexpected templates %#v", result.PromptTemplates)
	}
}

func TestLoadPromptTemplatesParseFailed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.md")
	if err := os.WriteFile(path, []byte("---\ndescription: [\n---\nBody"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := LoadPromptTemplates(NewLocalExecutionEnv(""), path)
	if len(result.PromptTemplates) != 0 {
		t.Fatalf("expected no templates, got %#v", result.PromptTemplates)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "parse_failed" {
		t.Fatalf("expected parse_failed diagnostic, got %#v", result.Diagnostics)
	}
}

func TestLoadSourcedPromptTemplates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.md")
	if err := os.WriteFile(path, []byte("Body"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := LoadSourcedPromptTemplates(NewLocalExecutionEnv(""), []SourcedPromptTemplateInput[string]{
		{Path: path, Source: "local"},
	})
	if len(result.PromptTemplates) != 1 {
		t.Fatalf("expected sourced template, got %#v", result.PromptTemplates)
	}
	if result.PromptTemplates[0].Source != "local" || result.PromptTemplates[0].PromptTemplate.Name != "a" {
		t.Fatalf("unexpected sourced template %#v", result.PromptTemplates[0])
	}
}
