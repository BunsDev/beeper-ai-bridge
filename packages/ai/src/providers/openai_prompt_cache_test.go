package providers

import (
	"strings"
	"testing"
)

func TestClampOpenAIPromptCacheKeyMatchesTypeScript(t *testing.T) {
	short := "session"
	if got := ClampOpenAIPromptCacheKey(short); got != short {
		t.Fatalf("expected short key unchanged, got %q", got)
	}
	long := strings.Repeat("a", 70)
	if got := ClampOpenAIPromptCacheKey(long); got != strings.Repeat("a", OpenAIPromptCacheKeyMaxLength) {
		t.Fatalf("expected rune-clamped key, got %q", got)
	}
	unicode := strings.Repeat("ø", 70)
	if got := ClampOpenAIPromptCacheKey(unicode); len([]rune(got)) != OpenAIPromptCacheKeyMaxLength {
		t.Fatalf("expected unicode key clamped by character, got %q", got)
	}
}

func TestClampOpenAIPromptCacheKeyPtrPreservesNil(t *testing.T) {
	if got := ClampOpenAIPromptCacheKeyPtr(nil); got != nil {
		t.Fatalf("expected nil key to stay nil, got %#v", got)
	}
	long := strings.Repeat("a", 70)
	got := ClampOpenAIPromptCacheKeyPtr(&long)
	if got == nil || *got != strings.Repeat("a", OpenAIPromptCacheKeyMaxLength) {
		t.Fatalf("expected clamped key pointer, got %#v", got)
	}
}
