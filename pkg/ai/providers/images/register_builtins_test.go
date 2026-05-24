package images

import (
	"testing"

	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func TestImageModelRegistryOnlyExposesRegisteredAPIs(t *testing.T) {
	RegisterBuiltInImagesAPIProviders()
	for _, provider := range ai.GetImageProviders() {
		for _, model := range ai.GetImageModels(provider) {
			if _, ok := ai.GetImagesAPIProvider(model.API); !ok {
				t.Fatalf("image model %s/%s uses unregistered api %s", provider, model.ID, model.API)
			}
		}
	}
}
