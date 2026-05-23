package providers

import (
	"testing"

	ai "github.com/earendil-works/pi-mono/packages/ai/src"
)

func TestBuildBaseOptions(t *testing.T) {
	temp := 0.5
	maxTokens := 42
	options := &ai.SimpleStreamOptions{
		StreamOptions: ai.StreamOptions{
			Temperature: &temp,
			MaxTokens:   &maxTokens,
			APIKey:      "options-key",
			SessionID:   "session-1",
		},
	}

	base := BuildBaseOptions(ai.Model{}, options, "explicit-key")
	if base.APIKey != "explicit-key" {
		t.Fatalf("expected explicit api key, got %q", base.APIKey)
	}
	if base.Temperature != &temp || base.MaxTokens != &maxTokens || base.SessionID != "session-1" {
		t.Fatalf("expected stream options to be copied: %#v", base)
	}
}

func TestBuildBaseOptionsUsesOptionsAPIKey(t *testing.T) {
	options := &ai.SimpleStreamOptions{StreamOptions: ai.StreamOptions{APIKey: "options-key"}}
	base := BuildBaseOptions(ai.Model{}, options, "")
	if base.APIKey != "options-key" {
		t.Fatalf("expected options api key, got %q", base.APIKey)
	}
}

func TestClampReasoning(t *testing.T) {
	xhigh := ai.ThinkingLevelXHigh
	clamped := ClampReasoning(&xhigh)
	if clamped == nil || *clamped != ai.ThinkingLevelHigh {
		t.Fatalf("expected xhigh to clamp to high, got %#v", clamped)
	}
}

func TestAdjustMaxTokensForThinking(t *testing.T) {
	baseMaxTokens := 2000
	adjusted := AdjustMaxTokensForThinking(&baseMaxTokens, 10000, ai.ThinkingLevelLow, nil)
	if adjusted.MaxTokens != 4048 || adjusted.ThinkingBudget != 2048 {
		t.Fatalf("unexpected adjusted tokens: %#v", adjusted)
	}

	capped := AdjustMaxTokensForThinking(nil, 3000, ai.ThinkingLevelHigh, nil)
	if capped.MaxTokens != 3000 || capped.ThinkingBudget != 1976 {
		t.Fatalf("unexpected capped tokens: %#v", capped)
	}
}
