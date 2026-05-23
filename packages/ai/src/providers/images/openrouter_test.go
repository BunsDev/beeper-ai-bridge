package images

import (
	"context"
	"errors"
	"testing"

	ai "github.com/earendil-works/pi-mono/packages/ai/src"
)

func TestBuildOpenRouterImagesParams(t *testing.T) {
	model := ai.ImagesModel{ID: "openrouter/auto", Output: []string{"image", "text"}}
	params := buildOpenRouterImagesParams(model, ai.ImagesContext{Input: []ai.ContentBlock{
		{Type: "text", Text: "hello"},
		{Type: "image", MimeType: "image/png", Data: "abc"},
	}})
	if params["model"] != "openrouter/auto" || params["stream"] != false {
		t.Fatalf("unexpected params: %#v", params)
	}
	modalities := params["modalities"].([]string)
	if len(modalities) != 2 || modalities[0] != "image" || modalities[1] != "text" {
		t.Fatalf("unexpected modalities: %#v", modalities)
	}
	messages := params["messages"].([]map[string]any)
	content := messages[0]["content"].([]map[string]any)
	if content[0]["text"] != "hello" {
		t.Fatalf("unexpected text content: %#v", content[0])
	}
	imageURL := content[1]["image_url"].(map[string]any)["url"]
	if imageURL != "data:image/png;base64,abc" {
		t.Fatalf("unexpected image URL: %#v", imageURL)
	}
}

func TestParseOpenRouterImagesChoice(t *testing.T) {
	output := ai.AssistantImages{}
	parseOpenRouterImagesChoice(&output, map[string]any{
		"choices": []any{map[string]any{
			"message": map[string]any{
				"content": "caption",
				"images":  []any{map[string]any{"image_url": map[string]any{"url": "data:image/png;base64,abc"}}},
			},
		}},
	})
	if len(output.Output) != 2 || output.Output[0].Text != "caption" || output.Output[1].Data != "abc" {
		t.Fatalf("unexpected output: %#v", output.Output)
	}
}

func TestGenerateImagesOpenRouterPayloadReplacementRejectsUnsupportedBody(t *testing.T) {
	model := ai.ImagesModel{API: ai.ImagesApiOpenRouter, Provider: ai.ImagesProviderOpenRouter, ID: "openrouter/auto"}
	output := GenerateImagesOpenRouter(context.Background(), model, ai.ImagesContext{}, ai.ImagesOptions{
		APIKey: "key",
		OnPayload: func(payload any, model ai.ImagesModel) (any, bool, error) {
			return []any{}, true, nil
		},
	})
	if output.StopReason != ai.ImagesStopReasonError || output.ErrorMessage != "onPayload returned unsupported OpenAI request body" {
		t.Fatalf("unexpected output: %#v", output)
	}
}

func TestGenerateImagesOpenRouterPayloadError(t *testing.T) {
	model := ai.ImagesModel{API: ai.ImagesApiOpenRouter, Provider: ai.ImagesProviderOpenRouter, ID: "openrouter/auto"}
	output := GenerateImagesOpenRouter(context.Background(), model, ai.ImagesContext{}, ai.ImagesOptions{
		APIKey: "key",
		OnPayload: func(payload any, model ai.ImagesModel) (any, bool, error) {
			return nil, false, errors.New("payload failed")
		},
	})
	if output.StopReason != ai.ImagesStopReasonError || output.ErrorMessage != "payload failed" {
		t.Fatalf("unexpected output: %#v", output)
	}
}
