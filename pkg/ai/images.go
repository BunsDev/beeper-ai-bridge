package ai

import (
	"context"
	"fmt"
)

func GenerateImages(ctx context.Context, model ImagesModel, imageContext ImagesContext, options ImagesOptions) AssistantImages {
	provider, ok := GetImagesAPIProvider(model.API)
	if !ok {
		panic(fmt.Sprintf("No API provider registered for api: %s", model.API))
	}
	return provider.GenerateImages(ctx, model, imageContext, options)
}
