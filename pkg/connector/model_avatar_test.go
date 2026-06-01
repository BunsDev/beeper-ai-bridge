package connector

import (
	"context"
	"net/http"
	"net/http/httptest"
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
		{name: "catalog provider logo wins over route", model: ai.Model{ID: "openrouter/anthropic/claude-opus-4.8-fast", Provider: ai.ProviderOpenRouter, Compat: map[string]any{"provider_logo_url": "/models/providers/anthropic.png"}}, wantKey: "anthropic"},
		{name: "catalog runtime model wins over route", model: ai.Model{ID: "openrouter/z-ai/glm-5", Provider: ai.ProviderOpenRouter, Compat: map[string]any{"runtime_model": "z-ai/glm-5"}}, wantKey: "zai"},
		{name: "catalog family wins over route", model: ai.Model{ID: "openrouter/xiaomi/mimo-v2.5", Provider: ai.ProviderOpenRouter, Compat: map[string]any{"family": "mimo"}}, wantKey: "xiaomi"},
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

func TestModelAvatarFetchesCatalogProviderLogoFromAIServices(t *testing.T) {
	const pngHeader = "\x89PNG\r\n\x1a\n"
	requestPath := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		if requestPath != "/models/providers/qwen.png" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte(pngHeader))
	}))
	defer server.Close()

	avatar := modelAvatar(aiid.ProviderConfig{BaseURL: server.URL}, ai.Model{
		ID:       "qwen/qwen3-max",
		Provider: ai.ProviderOpenRouter,
		Compat:   map[string]any{"provider_logo_url": "/models/providers/qwen.png"},
	})
	if avatar == nil {
		t.Fatal("expected model avatar")
	}
	if got := string(avatar.ID); got != "ai-model-provider-url:/models/providers/qwen.png" {
		t.Fatalf("unexpected avatar ID %q", got)
	}
	data, err := avatar.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != pngHeader {
		t.Fatalf("unexpected avatar bytes %q", string(data))
	}
	if requestPath != "/models/providers/qwen.png" {
		t.Fatalf("unexpected provider logo request path %q", requestPath)
	}
}

func TestAIServicesProviderLogoURL(t *testing.T) {
	got, err := aiServicesProviderLogoURL("https://ai-services.example/dev", "/models/providers/anthropic.png")
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://ai-services.example/dev/models/providers/anthropic.png"; got != want {
		t.Fatalf("unexpected logo URL %q, want %q", got, want)
	}

	got, err = aiServicesProviderLogoURL("https://ai-services.example", "models/providers/zai.png?cache=1")
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://ai-services.example/models/providers/zai.png?cache=1"; got != want {
		t.Fatalf("unexpected logo URL %q, want %q", got, want)
	}
}
