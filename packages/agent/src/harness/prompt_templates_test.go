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
	result := LoadPromptTemplates(dir)
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
