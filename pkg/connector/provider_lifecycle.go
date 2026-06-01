package connector

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/id"

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

func (c *Connector) VerifyProviderConfig(ctx context.Context, login *bridgev2.UserLogin, input ProviderInput) (aiid.ProviderConfig, error) {
	if login == nil {
		return aiid.ProviderConfig{}, fmt.Errorf("login is required")
	}
	return c.providerConfigFromInput(ctx, login.UserMXID, input)
}

func (c *Connector) providerConfigFromInput(ctx context.Context, userMXID id.UserID, input ProviderInput) (aiid.ProviderConfig, error) {
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
	provider, inferredAPI := inferProviderRoute(input.ID, input.BaseURL)
	if input.API == "" {
		input.API = inferredAPI
		if input.DisplayName == "" {
			input.DisplayName = providerDisplayName(string(provider))
		}
	}
	if !supportedProviderLoginAPI(input.API) || !providerAPIAllowed(provider, input.API) {
		return aiid.ProviderConfig{}, fmt.Errorf("provider %s with API %s is not supported", input.ID, input.API)
	}
	if input.BaseURL == "" || input.APIKey == "" {
		return aiid.ProviderConfig{}, fmt.Errorf("base_url and api_key are required")
	}
	if input.DisplayName == "" {
		input.DisplayName = providerDisplayName(input.ID)
	}
	providerConfig := aiid.ProviderConfig{
		ID:           input.ID,
		DisplayName:  input.DisplayName,
		API:          input.API,
		Provider:     configuredModelProvider(input.ID, provider),
		BaseURL:      input.BaseURL,
		APIKey:       input.APIKey,
		DefaultModel: input.DefaultModel,
	}
	models, err := c.aiServicesCatalogModelsForUserProvider(ctx, userMXID, providerConfig)
	if err != nil {
		return aiid.ProviderConfig{}, err
	}
	if len(models) == 0 {
		return aiid.ProviderConfig{}, fmt.Errorf("AI Services catalog has no models for provider %s", providerConfig.Provider)
	}
	providerConfig.Models = models
	if providerConfig.DefaultModel == "" {
		providerConfig.DefaultModel = models[0].ID
	} else if resolved, ok := resolveProviderModelID(aiid.ProviderConfig{Models: models}, providerConfig.DefaultModel); ok {
		providerConfig.DefaultModel = resolved
	} else {
		return aiid.ProviderConfig{}, fmt.Errorf("model %s was not returned by AI Services for provider %s", providerConfig.DefaultModel, input.ID)
	}
	return providerConfig, nil
}

func (c *Connector) aiServicesCatalogModelsForUserProvider(ctx context.Context, userMXID id.UserID, provider aiid.ProviderConfig) ([]ai.Model, error) {
	client := &Client{
		Main: c,
		UserLogin: &bridgev2.UserLogin{
			UserLogin: &database.UserLogin{UserMXID: userMXID},
		},
	}
	return client.aiServicesCatalogModels(ctx, provider)
}

func providerAPIAllowed(provider ai.Provider, api ai.Api) bool {
	switch provider {
	case ai.ProviderOpenAI:
		return api == ai.ApiOpenAIResponses || api == ai.ApiOpenAICompletions || api == ai.ApiOpenAICodexResponses
	case ai.ProviderOpenRouter:
		return api == ai.ApiOpenAICompletions
	case ai.ProviderAnthropic:
		return api == ai.ApiAnthropicMessages
	case ai.ProviderGoogleVertex:
		return api == ai.ApiGoogleVertex
	default:
		return false
	}
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
	ids := slices.SortedFunc(maps.Keys(providers), compareProviderID)
	out := make([]ProviderResponse, 0, len(ids))
	for _, id := range ids {
		out = append(out, providerResponse(providers[id]))
	}
	return out
}

func compareProviderID(a, b string) int {
	if a == b {
		return 0
	}
	if a == aiid.DefaultProvider {
		return -1
	}
	if b == aiid.DefaultProvider {
		return 1
	}
	return cmp.Compare(a, b)
}
