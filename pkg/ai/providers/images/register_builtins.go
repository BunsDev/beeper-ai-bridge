package images

import ai "github.com/beeper/ai-bridge/pkg/ai"

func RegisterBuiltInImagesAPIProviders() {
	ai.RegisterImagesAPIProvider(ai.ImagesAPIProvider{
		API:            ai.ImagesApiOpenRouter,
		GenerateImages: GenerateImagesOpenRouter,
	})
}

func init() {
	RegisterBuiltInImagesAPIProviders()
}
