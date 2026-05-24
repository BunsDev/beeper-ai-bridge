package ai

import (
	"context"
	"fmt"
)

type registeredAPIProvider struct {
	provider APIProvider
	sourceID string
}

var apiProviders = map[Api]registeredAPIProvider{}

func RegisterAPIProvider(api Api, streamSimple StreamFunction) {
	RegisterAPIProviderWithSource(APIProvider{
		API: api,
		Stream: func(ctx context.Context, model Model, llmContext Context, options StreamOptions) *AssistantMessageEventStream {
			return streamSimple(ctx, model, llmContext, SimpleStreamOptions{StreamOptions: options})
		},
		StreamSimple: streamSimple,
	}, "")
}

func RegisterAPIProviderWithSource(provider APIProvider, sourceID string) {
	stream := provider.Stream
	streamSimple := provider.StreamSimple
	if stream == nil && streamSimple != nil {
		stream = func(ctx context.Context, model Model, llmContext Context, options StreamOptions) *AssistantMessageEventStream {
			return streamSimple(ctx, model, llmContext, SimpleStreamOptions{StreamOptions: options})
		}
	}
	if streamSimple == nil && stream != nil {
		streamSimple = func(ctx context.Context, model Model, llmContext Context, options SimpleStreamOptions) *AssistantMessageEventStream {
			return stream(ctx, model, llmContext, options.StreamOptions)
		}
	}
	apiProviders[provider.API] = registeredAPIProvider{
		provider: APIProvider{API: provider.API, Stream: wrapAPIStream(provider.API, stream), StreamSimple: wrapAPIStreamSimple(provider.API, streamSimple)},
		sourceID: sourceID,
	}
}

func GetAPIProvider(api Api) (APIProvider, bool) {
	entry, ok := apiProviders[api]
	return entry.provider, ok
}

func GetAPIProviders() []APIProvider {
	providers := make([]APIProvider, 0, len(apiProviders))
	for _, entry := range apiProviders {
		providers = append(providers, entry.provider)
	}
	return providers
}

func UnregisterAPIProvider(api Api) {
	delete(apiProviders, api)
}

func UnregisterAPIProviders(sourceID string) {
	for api, entry := range apiProviders {
		if entry.sourceID == sourceID {
			delete(apiProviders, api)
		}
	}
}

func ClearAPIProviders() {
	apiProviders = map[Api]registeredAPIProvider{}
}

func wrapAPIStream(api Api, streamFn APIStreamFunction) APIStreamFunction {
	return func(ctx context.Context, model Model, llmContext Context, options StreamOptions) *AssistantMessageEventStream {
		if model.API != api {
			panic(fmt.Sprintf("Mismatched api: %s expected %s", model.API, api))
		}
		return streamFn(ctx, model, llmContext, options)
	}
}

func wrapAPIStreamSimple(api Api, streamFn APIStreamSimpleFunction) APIStreamSimpleFunction {
	return func(ctx context.Context, model Model, llmContext Context, options SimpleStreamOptions) *AssistantMessageEventStream {
		if model.API != api {
			panic(fmt.Sprintf("Mismatched api: %s expected %s", model.API, api))
		}
		return streamFn(ctx, model, llmContext, options)
	}
}
