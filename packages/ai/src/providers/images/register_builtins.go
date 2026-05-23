package images

import ai "github.com/earendil-works/pi-mono/packages/ai/src"

func RegisterBuiltInImagesAPIProviders() {
	ai.RegisterImagesAPIProvider(ai.ImagesAPIProvider{
		API:            ai.ImagesApiOpenRouter,
		GenerateImages: GenerateImagesOpenRouter,
	})
}
