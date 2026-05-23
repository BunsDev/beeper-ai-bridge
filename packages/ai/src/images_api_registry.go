package ai

import (
	"context"
	"fmt"
)

type ImagesFunction func(context.Context, ImagesModel, ImagesContext, ImagesOptions) AssistantImages

type ImagesAPIProvider struct {
	API            ImagesApi
	GenerateImages ImagesFunction
}

type registeredImagesAPIProvider struct {
	provider ImagesAPIProvider
	sourceID string
}

var imagesAPIProviderRegistry = map[ImagesApi]registeredImagesAPIProvider{}

func RegisterImagesAPIProvider(provider ImagesAPIProvider, sourceID ...string) {
	id := ""
	if len(sourceID) > 0 {
		id = sourceID[0]
	}
	api := provider.API
	generateImages := provider.GenerateImages
	imagesAPIProviderRegistry[api] = registeredImagesAPIProvider{
		provider: ImagesAPIProvider{
			API: api,
			GenerateImages: func(ctx context.Context, model ImagesModel, imageContext ImagesContext, options ImagesOptions) AssistantImages {
				if model.API != api {
					panic(fmt.Sprintf("Mismatched api: %s expected %s", model.API, api))
				}
				return generateImages(ctx, model, imageContext, options)
			},
		},
		sourceID: id,
	}
}

func GetImagesAPIProvider(api ImagesApi) (ImagesAPIProvider, bool) {
	registered, ok := imagesAPIProviderRegistry[api]
	return registered.provider, ok
}
