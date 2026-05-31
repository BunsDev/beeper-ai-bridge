package connector

import (
	"context"
	"net/http"
	"testing"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
)

func TestModelAvatarUsesEmbeddedPNGAssets(t *testing.T) {
	tests := []struct {
		name     string
		provider aiid.ProviderConfig
		model    ai.Model
		wantKey  string
	}{
		{name: "anthropic", model: ai.Model{ID: "claude-sonnet-4.5", Provider: ai.ProviderAnthropic}, wantKey: "anthropic"},
		{name: "google vertex", model: ai.Model{ID: "gemini-3-pro", Provider: ai.ProviderGoogleVertex}, wantKey: "google"},
		{name: "openrouter own model", model: ai.Model{ID: "openrouter/owl-alpha", Provider: ai.ProviderOpenRouter}, wantKey: "openrouter"},
		{name: "openrouter routed anthropic model", model: ai.Model{ID: "anthropic/claude-sonnet-4.5", Provider: ai.ProviderOpenRouter}, wantKey: "anthropic"},
		{name: "openrouter routed bare claude model", model: ai.Model{ID: "claude-sonnet-4-5", Provider: ai.ProviderOpenRouter}, wantKey: "anthropic"},
		{name: "openrouter routed openai model", model: ai.Model{ID: "openai/gpt-5", Provider: ai.ProviderOpenRouter}, wantKey: "openai"},
		{name: "openai", model: ai.Model{ID: "gpt-5", Provider: ai.ProviderOpenAI}, wantKey: "openai"},
		{name: "deepseek", model: ai.Model{ID: "deepseek-chat", Provider: ai.ProviderDeepSeek}, wantKey: "deepseek"},
		{name: "xai", model: ai.Model{ID: "grok-4", Provider: ai.ProviderXAI}, wantKey: "xai"},
		{name: "zai", model: ai.Model{ID: "glm-4.5", Provider: ai.ProviderZai}, wantKey: "zai"},
		{name: "fallback", provider: aiid.ProviderConfig{ID: "beeper"}, model: ai.Model{ID: "unknown"}, wantKey: "beeper-ai"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			avatar := modelAvatar(tt.provider, tt.model)
			if avatar == nil {
				t.Fatal("expected model avatar")
			}
			if got := string(avatar.ID); got != "ai-model-provider:"+tt.wantKey {
				t.Fatalf("unexpected avatar ID %q", got)
			}
			if avatar.Get == nil {
				t.Fatal("expected embedded avatar fetcher")
			}
			data, err := avatar.Get(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got := http.DetectContentType(data); got != "image/png" {
				t.Fatalf("expected PNG avatar, got %s", got)
			}
		})
	}
}
