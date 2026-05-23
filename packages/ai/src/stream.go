package ai

import (
	"context"
	"fmt"
)

func Stream(ctx context.Context, model Model, llmContext Context, options StreamOptions) *AssistantMessageEventStream {
	provider, ok := apiProviders[model.API]
	if !ok {
		panic(fmt.Sprintf("No API provider registered for api: %s", model.API))
	}
	return provider.provider.Stream(ctx, model, llmContext, options)
}

func Complete(ctx context.Context, model Model, llmContext Context, options StreamOptions) Message {
	return Stream(ctx, model, llmContext, options).Result()
}

func StreamSimple(ctx context.Context, model Model, llmContext Context, options SimpleStreamOptions) *AssistantMessageEventStream {
	provider, ok := apiProviders[model.API]
	if !ok {
		panic(fmt.Sprintf("No API provider registered for api: %s", model.API))
	}
	return provider.provider.StreamSimple(ctx, model, llmContext, options)
}

func CompleteSimple(ctx context.Context, model Model, llmContext Context, options SimpleStreamOptions) Message {
	return StreamSimple(ctx, model, llmContext, options).Result()
}
