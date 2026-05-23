package providers

import (
	"context"

	ai "github.com/earendil-works/pi-mono/packages/ai/src"
)

func RegisterBuiltInAPIProviders() {
	ai.RegisterAPIProviderWithSource(ai.APIProvider{API: ai.ApiOpenAICompletions, Stream: func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.StreamOptions) *ai.AssistantMessageEventStream {
		return StreamOpenAICompletions(ctx, model, llmContext, OpenAICompletionsOptions{StreamOptions: options})
	}, StreamSimple: StreamSimpleOpenAICompletions}, "builtins")
	ai.RegisterAPIProviderWithSource(ai.APIProvider{API: ai.ApiOpenAIResponses, Stream: func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.StreamOptions) *ai.AssistantMessageEventStream {
		return StreamOpenAIResponses(ctx, model, llmContext, OpenAIResponsesOptions{StreamOptions: options})
	}, StreamSimple: StreamSimpleOpenAIResponses}, "builtins")
	ai.RegisterAPIProviderWithSource(ai.APIProvider{API: ai.ApiOpenAICodexResponses, Stream: func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.StreamOptions) *ai.AssistantMessageEventStream {
		return StreamOpenAICodexResponses(ctx, model, llmContext, OpenAICodexResponsesOptions{OpenAIResponsesOptions: OpenAIResponsesOptions{StreamOptions: options}})
	}, StreamSimple: StreamSimpleOpenAICodexResponses}, "builtins")
}

func ResetAPIProviders() {
	ai.ClearAPIProviders()
	RegisterBuiltInAPIProviders()
}

func init() {
	RegisterBuiltInAPIProviders()
}
