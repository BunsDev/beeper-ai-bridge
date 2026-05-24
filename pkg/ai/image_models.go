package ai

func GetImageModel(provider ImagesProvider, modelID string) (ImagesModel, bool) {
	providerModels := ImageModels[provider]
	if providerModels == nil {
		return ImagesModel{}, false
	}
	model, ok := providerModels[modelID]
	return model, ok
}

func GetImageProviders() []ImagesProvider {
	return append([]ImagesProvider{}, imageModelProviderOrder...)
}

func GetImageModels(provider ImagesProvider) []ImagesModel {
	providerModels := ImageModels[provider]
	if providerModels == nil {
		return nil
	}
	models := make([]ImagesModel, 0, len(providerModels))
	for _, id := range imageModelIDOrder[provider] {
		models = append(models, providerModels[id])
	}
	return models
}
