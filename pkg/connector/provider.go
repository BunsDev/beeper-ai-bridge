package connector

import (
	"context"
	"fmt"

	"github.com/beeper/ai-bridge/pkg/agent/harness"
	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
)

func (c *Connector) ModelForProvider(provider aiid.ProviderConfig, modelID string) ai.Model {
	for _, model := range provider.Models {
		if model.ID == modelID {
			return normalizeProviderModel(model, provider)
		}
	}
	if model, ok := ai.GetModel(provider.Provider, modelID); ok {
		return normalizeProviderModel(model, provider)
	}
	return normalizeProviderModel(ai.Model{
		ID:            modelID,
		Name:          modelID,
		API:           provider.API,
		Provider:      provider.Provider,
		BaseURL:       provider.BaseURL,
		Input:         []string{"text", "image"},
		ContextWindow: 128000,
		MaxTokens:     32000,
	}, provider)
}

func normalizeProviderModel(model ai.Model, provider aiid.ProviderConfig) ai.Model {
	if model.API == "" {
		model.API = provider.API
	}
	if model.Provider == "" {
		model.Provider = provider.Provider
	}
	if model.BaseURL == "" {
		model.BaseURL = provider.BaseURL
	}
	if model.Name == "" {
		model.Name = model.ID
	}
	if len(model.Input) == 0 {
		model.Input = []string{"text"}
	}
	model.BaseURL = normalizeResponsesBaseURL(model.BaseURL)
	return model
}

func (cl *Client) authForProvider(provider aiid.ProviderConfig) func(context.Context, ai.Model) (*harness.AgentHarnessAuth, error) {
	return func(ctx context.Context, model ai.Model) (*harness.AgentHarnessAuth, error) {
		apiKey := provider.APIKey
		if provider.ID == aiid.DefaultProvider {
			apiKey = cl.Main.AppServiceToken
		}
		if apiKey == "" {
			return nil, fmt.Errorf("missing API key for provider %s", provider.ID)
		}
		return &harness.AgentHarnessAuth{
			APIKey:  apiKey,
			Headers: provider.Headers,
		}, nil
	}
}

func isImageModel(model ai.Model) bool {
	for _, input := range model.Input {
		if input == "image" {
			return true
		}
	}
	return false
}
