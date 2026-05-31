package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func TestGoogleBeeperProxyEndpointStripsCatalogProviderPrefix(t *testing.T) {
	model := ai.Model{
		ID:       "google/gemini-3.1-pro-preview",
		API:      ai.ApiGoogleGenerativeAI,
		Provider: ai.ProviderGoogle,
		BaseURL:  "https://ai-services.beeper.localtest.me/proxy/google/v1",
	}
	endpoint, err := googleEndpoint(model)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://ai-services.beeper.localtest.me/proxy/google/v1/models/gemini-3.1-pro-preview:streamGenerateContent?alt=sse"
	if endpoint != want {
		t.Fatalf("endpoint = %q, want %q", endpoint, want)
	}
}

func TestGoogleProviderIsNotRegistered(t *testing.T) {
	if provider, ok := ai.GetAPIProvider(ai.ApiGoogleGenerativeAI); ok {
		t.Fatalf("google-generative-ai should not be registered for Beeper runtime, got %#v", provider)
	}
}

func TestGoogleBeeperProxyUsesBearerAuth(t *testing.T) {
	var gotAuth string
	var gotGoogleKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotGoogleKey = r.Header.Get("X-Goog-Api-Key")
	}))
	defer server.Close()

	resp, err := doGoogleRequest(context.Background(), ai.Model{
		ID:       "google/gemini-3.1-pro-preview",
		API:      ai.ApiGoogleGenerativeAI,
		Provider: ai.ProviderGoogle,
		BaseURL:  server.URL + "/proxy/google/v1",
	}, GoogleOptions{StreamOptions: ai.StreamOptions{APIKey: "proxy-token"}}, map[string]any{"contents": []map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if gotAuth != "Bearer proxy-token" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotGoogleKey != "" {
		t.Fatalf("X-Goog-Api-Key = %q, want empty", gotGoogleKey)
	}
}

func TestGoogleParamsRequestImageResponseModalities(t *testing.T) {
	params := BuildGoogleParams(
		ai.Model{ID: "google/gemini-3.1-flash-image-preview", Output: []string{"image", "text"}},
		ai.Context{Messages: []ai.Message{{Role: "user", Content: "draw Amsterdam"}}},
		GoogleOptions{},
	)
	config, ok := params["generationConfig"].(map[string]any)
	if !ok {
		t.Fatalf("expected generation config, got %#v", params)
	}
	modalities, ok := config["responseModalities"].([]string)
	if !ok || len(modalities) != 2 || modalities[0] != "TEXT" || modalities[1] != "IMAGE" {
		t.Fatalf("unexpected response modalities %#v", config["responseModalities"])
	}
}
