package ai

import (
	"context"
	"testing"
)

func TestImageModelRegistryGeneratedSurface(t *testing.T) {
	providers := GetImageProviders()
	if len(providers) != 1 || providers[0] != ImagesProviderOpenRouter {
		t.Fatalf("unexpected image providers: %#v", providers)
	}
	model, ok := GetImageModel(ImagesProviderOpenRouter, "openai/gpt-5-image")
	if !ok {
		t.Fatal("expected generated image model")
	}
	if model.API != ImagesApiOpenRouter || model.Provider != ImagesProviderOpenRouter {
		t.Fatalf("unexpected image model metadata: %#v", model)
	}
	models := GetImageModels(ImagesProviderOpenRouter)
	if len(models) == 0 || models[0].ID != "black-forest-labs/flux.2-flex" {
		t.Fatalf("unexpected image model order: %#v", models)
	}
}

func TestGenerateImagesMissingProviderPanics(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("expected missing provider panic")
		}
	}()
	GenerateImages(context.Background(), ImagesModel{API: "missing"}, ImagesContext{}, ImagesOptions{})
}

func TestImagesAPIProviderMismatchedAPIPanics(t *testing.T) {
	RegisterImagesAPIProvider(ImagesAPIProvider{
		API: "registered-images",
		GenerateImages: func(context.Context, ImagesModel, ImagesContext, ImagesOptions) AssistantImages {
			return AssistantImages{}
		},
	}, "test")
	provider, ok := GetImagesAPIProvider("registered-images")
	if !ok {
		t.Fatal("expected registered image provider")
	}
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("expected mismatched api panic")
		}
	}()
	provider.GenerateImages(context.Background(), ImagesModel{API: "other-images"}, ImagesContext{}, ImagesOptions{})
}
