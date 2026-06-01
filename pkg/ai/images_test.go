package ai

import (
	"context"
	"testing"
)

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
