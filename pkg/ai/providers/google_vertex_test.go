package providers

import (
	"testing"

	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func TestGoogleVertexBeeperProxyEndpointDoesNotRequireGCPProject(t *testing.T) {
	model := ai.Model{
		ID:       "gemini-2.5-flash-lite",
		API:      ai.ApiGoogleVertex,
		Provider: ai.ProviderGoogleVertex,
		BaseURL:  "https://ai-services.beeper.localtest.me/proxy/vertex",
	}
	endpoint, err := googleVertexEndpoint(model, GoogleVertexOptions{}, false, true)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://ai-services.beeper.localtest.me/proxy/vertex/v1/publishers/google/models/gemini-2.5-flash-lite:streamGenerateContent?alt=sse"
	if endpoint != want {
		t.Fatalf("endpoint = %q, want %q", endpoint, want)
	}
}

func TestGoogleVertexParamsRequestImageResponseModalities(t *testing.T) {
	params := BuildGoogleVertexParams(
		ai.Model{ID: "google/gemini-3.1-flash-image-preview", Output: []string{"image", "text"}},
		ai.Context{Messages: []ai.Message{{Role: "user", Content: "draw Amsterdam"}}},
		GoogleVertexOptions{},
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

func TestGoogleVertexStreamStateAddsInlineDataImagePart(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream()
	output := newAssistant(ai.Model{ID: "google/gemini-3.1-flash-image-preview"})
	state := newGoogleVertexStreamState()

	state.apply(stream, &output, ai.Model{}, map[string]any{
		"candidates": []any{map[string]any{
			"content": map[string]any{
				"parts": []any{map[string]any{
					"inlineData": map[string]any{"mimeType": "image/png", "data": "abc"},
				}},
			},
			"finishReason": "STOP",
		}},
	})

	blocks := output.Content.([]ai.ContentBlock)
	if len(blocks) != 1 || blocks[0].Type != "image" || blocks[0].MimeType != "image/png" || blocks[0].Data != "abc" {
		t.Fatalf("unexpected blocks %#v", blocks)
	}
}
