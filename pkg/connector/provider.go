package connector

import (
	"context"
	"fmt"
	"os"
	"strings"

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
	return normalizeProviderModel(modelForProviderCatalog(provider, modelID), provider)
}

func modelForProviderCatalog(provider aiid.ProviderConfig, modelID string) ai.Model {
	if model, ok := ai.GetModel(provider.Provider, modelID); ok {
		return model
	}
	return ai.Model{
		ID:            modelID,
		Name:          modelID,
		API:           provider.API,
		Provider:      provider.Provider,
		BaseURL:       provider.BaseURL,
		Input:         []string{"text", "image"},
		ContextWindow: 128000,
		MaxTokens:     32000,
	}
}

func normalizeProviderModel(model ai.Model, provider aiid.ProviderConfig) ai.Model {
	if provider.API != "" {
		model.API = provider.API
	} else if model.API == "" {
		model.API = provider.API
	}
	if model.Provider == "" {
		model.Provider = provider.Provider
	}
	if provider.BaseURL != "" {
		model.BaseURL = provider.BaseURL
	} else if model.BaseURL == "" {
		model.BaseURL = provider.BaseURL
	}
	if model.Name == "" {
		model.Name = model.ID
	}
	if len(model.Input) == 0 {
		model.Input = []string{"text"}
	}
	if override, ok := provider.ModelOverrides[model.ID]; ok {
		model = applyModelOverride(model, override)
	}
	model.BaseURL = normalizeResponsesBaseURL(model.BaseURL)
	return model
}

func applyModelOverride(model ai.Model, override aiid.ModelOverride) ai.Model {
	if override.Name != "" {
		model.Name = override.Name
	}
	if override.API != "" {
		model.API = override.API
	}
	if override.BaseURL != "" {
		model.BaseURL = override.BaseURL
	}
	if override.Reasoning != nil {
		model.Reasoning = *override.Reasoning
	}
	if len(override.Input) > 0 {
		model.Input = override.Input
	}
	if override.ContextWindow > 0 {
		model.ContextWindow = override.ContextWindow
	}
	if override.MaxTokens > 0 {
		model.MaxTokens = override.MaxTokens
	}
	if len(override.Headers) > 0 {
		model.Headers = mergeStringMaps(model.Headers, override.Headers)
	}
	if len(override.Compat) > 0 {
		model.Compat = mergeAnyMaps(model.Compat, override.Compat)
	}
	return model
}

func mergeStringMaps(base map[string]string, override map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range base {
		out[key] = value
	}
	for key, value := range override {
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeAnyMaps(base map[string]any, override map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range base {
		out[key] = value
	}
	for key, value := range override {
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (cl *Client) authForProvider(provider aiid.ProviderConfig) func(context.Context, ai.Model) (*harness.AgentHarnessAuth, error) {
	return func(ctx context.Context, model ai.Model) (*harness.AgentHarnessAuth, error) {
		apiKey := resolveConfiguredAPIKey(provider.APIKey)
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

func resolveConfiguredAPIKey(apiKey string) string {
	if envName, ok := strings.CutPrefix(apiKey, "env:"); ok {
		return os.Getenv(strings.TrimSpace(envName))
	}
	return apiKey
}

func isImageModel(model ai.Model) bool {
	for _, input := range model.Input {
		if input == "image" {
			return true
		}
	}
	return false
}
