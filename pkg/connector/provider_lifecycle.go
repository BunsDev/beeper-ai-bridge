package connector

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"maunium.net/go/mautrix/bridgev2"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
)

var (
	errProviderReadOnly = errors.New("provider is managed by Beeper AI")
	errProviderNotFound = errors.New("provider not found")
)

type ProviderInput struct {
	ID           string `json:"id"`
	DisplayName  string `json:"display_name,omitempty"`
	API          ai.Api `json:"api"`
	BaseURL      string `json:"base_url"`
	APIKey       string `json:"api_key"`
	DefaultModel string `json:"default_model,omitempty"`
}

type ProviderResponse struct {
	ID           string      `json:"id"`
	DisplayName  string      `json:"display_name"`
	API          ai.Api      `json:"api"`
	Provider     ai.Provider `json:"provider"`
	BaseURL      string      `json:"base_url"`
	DefaultModel string      `json:"default_model,omitempty"`
	Models       []ai.Model  `json:"models,omitempty"`
	ReadOnly     bool        `json:"read_only,omitempty"`
}

func (c *Connector) VerifyProviderConfig(ctx context.Context, input ProviderInput) (aiid.ProviderConfig, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.BaseURL = normalizeResponsesBaseURL(strings.TrimSpace(input.BaseURL))
	input.APIKey = strings.TrimSpace(input.APIKey)
	input.DefaultModel = strings.TrimSpace(input.DefaultModel)
	if input.ID == "" {
		return aiid.ProviderConfig{}, fmt.Errorf("provider id is required")
	}
	if input.ID == aiid.DefaultProvider {
		return aiid.ProviderConfig{}, fmt.Errorf("provider %q is managed by Beeper AI", aiid.DefaultProvider)
	}
	if input.API == "" {
		provider, api := inferProviderRoute(input.ID, input.BaseURL)
		input.API = api
		if input.DisplayName == "" {
			input.DisplayName = providerDisplayName(string(provider))
		}
	}
	if input.BaseURL == "" || input.APIKey == "" {
		return aiid.ProviderConfig{}, fmt.Errorf("base_url and api_key are required")
	}
	models, err := fetchProviderModels(ctx, input.API, input.ID, input.BaseURL, input.APIKey)
	if err != nil {
		return aiid.ProviderConfig{}, err
	}
	if input.DefaultModel == "" {
		input.DefaultModel = models[0].ID
	} else if !providerHasModel(aiid.ProviderConfig{Models: models}, input.DefaultModel) {
		return aiid.ProviderConfig{}, fmt.Errorf("model %s was not returned by provider %s", input.DefaultModel, input.ID)
	}
	provider, _ := inferProviderRoute(input.ID, input.BaseURL)
	if input.DisplayName == "" {
		input.DisplayName = providerDisplayName(input.ID)
	}
	return aiid.ProviderConfig{
		ID:           input.ID,
		DisplayName:  input.DisplayName,
		API:          input.API,
		Provider:     configuredModelProvider(input.ID, provider),
		BaseURL:      input.BaseURL,
		APIKey:       input.APIKey,
		DefaultModel: input.DefaultModel,
		Models:       models,
	}, nil
}

func (c *Connector) SaveProviderConfig(ctx context.Context, login *bridgev2.UserLogin, provider aiid.ProviderConfig) error {
	if provider.ID == "" {
		return fmt.Errorf("provider id is required")
	}
	if provider.ID == aiid.DefaultProvider {
		return fmt.Errorf("%w: %s", errProviderReadOnly, aiid.DefaultProvider)
	}
	if login == nil {
		return fmt.Errorf("login is required")
	}
	c.providerConfigMu.Lock()
	defer c.providerConfigMu.Unlock()
	if err := c.ensureAIChatsMetadata(ctx, login); err != nil {
		return err
	}
	meta, ok := login.Metadata.(*aiid.UserLoginMetadata)
	if !ok || meta == nil {
		return fmt.Errorf("login metadata is invalid")
	}
	if meta.Providers == nil {
		meta.Providers = map[string]aiid.ProviderConfig{}
	}
	meta.Providers[provider.ID] = provider
	if client, ok := login.Client.(*Client); ok {
		client.invalidateModelContactCache()
	}
	if err := login.Save(ctx); err != nil {
		return err
	}
	return c.connectUserLogin(ctx, login)
}

func (c *Connector) DeleteProvider(ctx context.Context, login *bridgev2.UserLogin, providerID string) error {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return fmt.Errorf("provider id is required")
	}
	if providerID == aiid.DefaultProvider {
		return fmt.Errorf("%w: %s", errProviderReadOnly, aiid.DefaultProvider)
	}
	if login == nil {
		return fmt.Errorf("login is required")
	}
	c.providerConfigMu.Lock()
	defer c.providerConfigMu.Unlock()
	if err := c.ensureAIChatsMetadata(ctx, login); err != nil {
		return err
	}
	meta, ok := login.Metadata.(*aiid.UserLoginMetadata)
	if !ok || meta == nil {
		return fmt.Errorf("login metadata is invalid")
	}
	if _, ok := meta.Providers[providerID]; !ok {
		return fmt.Errorf("%w: %s", errProviderNotFound, providerID)
	}
	delete(meta.Providers, providerID)
	if client, ok := login.Client.(*Client); ok {
		client.invalidateModelContactCache()
	}
	return login.Save(ctx)
}

func providerResponse(provider aiid.ProviderConfig) ProviderResponse {
	return ProviderResponse{
		ID:           provider.ID,
		DisplayName:  provider.DisplayName,
		API:          provider.API,
		Provider:     provider.Provider,
		BaseURL:      provider.BaseURL,
		DefaultModel: provider.DefaultModel,
		Models:       provider.Models,
		ReadOnly:     provider.ID == aiid.DefaultProvider,
	}
}

func sortedProviderResponses(providers map[string]aiid.ProviderConfig) []ProviderResponse {
	ids := make([]string, 0, len(providers))
	for id := range providers {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if ids[i] == aiid.DefaultProvider {
			return true
		}
		if ids[j] == aiid.DefaultProvider {
			return false
		}
		return ids[i] < ids[j]
	})
	out := make([]ProviderResponse, 0, len(ids))
	for _, id := range ids {
		out = append(out, providerResponse(providers[id]))
	}
	return out
}
