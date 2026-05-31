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
	completeSimple := provider.CompleteSimple
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
	if completeSimple == nil && streamSimple != nil {
		completeSimple = func(ctx context.Context, model Model, llmContext Context, options SimpleStreamOptions) Message {
			return streamSimple(ctx, model, llmContext, options).Result()
		}
	}
	if streamSimple == nil && completeSimple != nil {
		streamSimple = func(ctx context.Context, model Model, llmContext Context, options SimpleStreamOptions) *AssistantMessageEventStream {
			stream := NewAssistantMessageEventStream()
			go func() {
				message := completeSimple(ctx, model, llmContext, options)
				if message.StopReason == StopReasonError || message.StopReason == StopReasonAborted {
					stream.Push(AssistantMessageEvent{Type: "error", Reason: message.StopReason, Error: &message})
					return
				}
				stream.Push(AssistantMessageEvent{Type: "done", Reason: message.StopReason, Message: &message})
			}()
			return stream
		}
	}
	if stream == nil && streamSimple != nil {
		stream = func(ctx context.Context, model Model, llmContext Context, options StreamOptions) *AssistantMessageEventStream {
			return streamSimple(ctx, model, llmContext, SimpleStreamOptions{StreamOptions: options})
		}
	}
	apiProviders[provider.API] = registeredAPIProvider{
		provider: APIProvider{API: provider.API, Stream: wrapAPIStream(provider.API, stream), StreamSimple: wrapAPIStreamSimple(provider.API, streamSimple), CompleteSimple: wrapAPICompleteSimple(provider.API, completeSimple)},
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

func wrapAPICompleteSimple(api Api, completeFn APICompleteSimpleFunction) APICompleteSimpleFunction {
	return func(ctx context.Context, model Model, llmContext Context, options SimpleStreamOptions) Message {
		if model.API != api {
			panic(fmt.Sprintf("Mismatched api: %s expected %s", model.API, api))
		}
		return completeFn(ctx, model, llmContext, options)
	}
}
