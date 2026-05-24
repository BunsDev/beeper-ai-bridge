package utils

import "testing"

func TestSanitizeSurrogatesRemovesUnpairedSurrogates(t *testing.T) {
	input := string([]rune{'a', 0xD83D, 'b', 0xDC00, 'c'})
	if got := SanitizeSurrogates(input); got != "abc" {
		t.Fatalf("expected unpaired surrogates removed, got %q", got)
	}
}

func TestSanitizeSurrogatesPreservesNormalUnicode(t *testing.T) {
	input := "Hello 🙈 World"
	if got := SanitizeSurrogates(input); got != input {
		t.Fatalf("expected normal unicode preserved, got %q", got)
	}
}
