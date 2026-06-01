package connector

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/id"

	"github.com/beeper/ai-bridge/pkg/agent/harness"
	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
)

const aiServicesAppserviceTokenPrefix = "as::"

type aiServicesAppserviceToken struct {
	ASToken  string `json:"as_token"`
	Username string `json:"username"`
}

func (c *Connector) ModelForProvider(provider aiid.ProviderConfig, modelID string) ai.Model {
	if resolvedModelID, ok := resolveProviderModelID(provider, modelID); ok {
		modelID = resolvedModelID
	}
	for _, model := range provider.Models {
		if model.ID == modelID {
			return normalizeProviderModel(model, provider)
		}
	}
	return normalizeProviderModel(modelForProviderConfig(provider, modelID), provider)
}

func (cl *Client) resolveProvider(ctx context.Context, roomConfig RoomConfig) (aiid.ProviderConfig, string, error) {
	logCtx := zerolog.Ctx(ctx).With().
		Str("action", "ai_model_resolution").
		Str("requested_model", roomConfig.ModelID)
	if cl != nil && cl.UserLogin != nil {
		logCtx = logCtx.Str("login_id", string(cl.UserLogin.ID))
	}
	log := logCtx.Logger()
	ctx = log.WithContext(ctx)
	provider, modelID, err := cl.Main.ResolveProvider(ctx, cl.UserLogin, roomConfig)
	if err != nil {
		log.Err(err).Msg("Failed to resolve AI provider")
		return aiid.ProviderConfig{}, "", err
	}
	provider, err = cl.providerWithCatalogModelsStrict(ctx, provider)
	if err != nil {
		log.Err(err).Str("provider_id", provider.ID).Msg("Failed to load AI provider model catalog")
		return aiid.ProviderConfig{}, "", err
	}
	if len(provider.Models) == 0 {
		err := fmt.Errorf("AI model catalog is unavailable for provider %s", provider.ID)
		log.Err(err).Str("provider_id", provider.ID).Msg("AI provider model catalog is empty")
		return aiid.ProviderConfig{}, "", err
	}
	if resolvedModelID, ok := resolveProviderModelID(provider, modelID); ok {
		log.Debug().
			Str("provider_id", provider.ID).
			Str("provider", string(provider.Provider)).
			Str("model_id", resolvedModelID).
			Int("model_count", len(provider.Models)).
			Msg("Resolved AI provider")
		return provider, resolvedModelID, nil
	}
	if roomConfig.ModelID == "" {
		log.Debug().
			Str("provider_id", provider.ID).
			Str("provider", string(provider.Provider)).
			Str("model_id", provider.Models[0].ID).
			Int("model_count", len(provider.Models)).
			Msg("Resolved AI provider to catalog default model")
		return provider, provider.Models[0].ID, nil
	}
	err = fmt.Errorf("model %s is not available for provider %s", modelID, provider.ID)
	log.Err(err).Str("provider_id", provider.ID).Str("model_id", modelID).Msg("AI model is unavailable for provider")
	return aiid.ProviderConfig{}, "", err
}

func modelForProviderConfig(provider aiid.ProviderConfig, modelID string) ai.Model {
	input := []string{"text", "image"}
	if provider.ID == aiid.DefaultProvider {
		input = []string{"text"}
	}
	return ai.Model{
		ID:            modelID,
		Name:          modelID,
		API:           provider.API,
		Provider:      provider.Provider,
		BaseURL:       provider.BaseURL,
		Input:         input,
		ContextWindow: 128000,
		MaxTokens:     32000,
	}
}

func normalizeProviderModel(model ai.Model, provider aiid.ProviderConfig) ai.Model {
	keepModelRoute := (model.Provider != "" && model.Provider != provider.Provider) || (model.BaseURL != "" && model.BaseURL != provider.BaseURL)
	if provider.API != "" && !keepModelRoute {
		model.API = provider.API
	} else if model.API == "" {
		model.API = provider.API
	}
	if model.Provider == "" {
		model.Provider = provider.Provider
	}
	if provider.BaseURL != "" && !keepModelRoute {
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
	model.BaseURL = normalizeResponsesBaseURL(model.BaseURL)
	return model
}

func (cl *Client) authForProvider(provider aiid.ProviderConfig) func(context.Context, ai.Model) (*harness.AgentHarnessAuth, error) {
	return func(ctx context.Context, model ai.Model) (*harness.AgentHarnessAuth, error) {
		logCtx := zerolog.Ctx(ctx).With().
			Str("action", "ai_provider_auth").
			Str("provider_id", provider.ID).
			Str("provider", string(provider.Provider)).
			Str("model_id", model.ID)
		if cl != nil && cl.UserLogin != nil {
			logCtx = logCtx.Str("login_id", string(cl.UserLogin.ID))
		}
		log := logCtx.Logger()
		ctx = log.WithContext(ctx)
		var err error
		currentProvider := provider
		currentProvider, err = cl.refreshProviderIfNeeded(ctx, currentProvider)
		if err != nil {
			log.Err(err).Msg("Failed to refresh AI provider credentials")
			return nil, err
		}
		apiKey := resolveConfiguredAPIKey(currentProvider.APIKey)
		if currentProvider.ID == aiid.DefaultProvider {
			apiKey, err = cl.defaultProviderBearerToken()
			if err != nil {
				log.Err(err).Msg("Failed to build default AI provider token")
				return nil, err
			}
		}
		if apiKey == "" {
			err = fmt.Errorf("missing API key for provider %s", currentProvider.ID)
			log.Err(err).Msg("Missing AI provider credentials")
			return nil, err
		}
		return &harness.AgentHarnessAuth{
			APIKey:  apiKey,
			Headers: currentProvider.Headers,
		}, nil
	}
}

func (cl *Client) defaultProviderBearerToken() (string, error) {
	if cl == nil || cl.Main == nil {
		return "", fmt.Errorf("missing connector for default provider")
	}
	if cl.UserLogin == nil {
		return "", fmt.Errorf("missing user login for default provider")
	}
	return cl.Main.defaultProviderBearerToken(cl.UserLogin.UserMXID)
}

func (c *Connector) defaultProviderBearerToken(userMXID id.UserID) (string, error) {
	if c == nil || c.AppServiceToken == "" {
		return "", fmt.Errorf("missing appservice token for default provider")
	}
	username := userMXID.Localpart()
	if username == "" {
		return "", fmt.Errorf("missing Beeper username for default provider")
	}
	payload, err := json.Marshal(aiServicesAppserviceToken{
		ASToken:  c.AppServiceToken,
		Username: username,
	})
	if err != nil {
		return "", err
	}
	return aiServicesAppserviceTokenPrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

func (cl *Client) refreshProviderIfNeeded(ctx context.Context, provider aiid.ProviderConfig) (aiid.ProviderConfig, error) {
	if provider.ID != chatGPTProviderID || provider.RefreshToken == "" || provider.ExpiresAtMS == 0 {
		return provider, nil
	}
	if time.Now().Add(2 * time.Minute).Before(time.UnixMilli(provider.ExpiresAtMS)) {
		return provider, nil
	}
	if cl != nil {
		cl.providerAuthMu.Lock()
		defer cl.providerAuthMu.Unlock()
		if refreshed, ok := cl.savedProviderConfig(provider.ID); ok {
			provider = refreshed
			if provider.RefreshToken == "" || provider.ExpiresAtMS == 0 || time.Now().Add(2*time.Minute).Before(time.UnixMilli(provider.ExpiresAtMS)) {
				return provider, nil
			}
		}
	}
	log := zerolog.Ctx(ctx).With().
		Str("action", "ai_provider_auth").
		Str("provider_id", provider.ID).
		Time("expires_at", time.UnixMilli(provider.ExpiresAtMS)).
		Logger()
	log.Debug().Msg("Refreshing AI provider credentials")
	credentials, err := refreshChatGPTCredentials(ctx, provider.RefreshToken)
	if err != nil {
		log.Err(err).Msg("Failed to refresh AI provider credentials")
		return provider, err
	}
	provider.APIKey = credentials.AccessToken
	provider.RefreshToken = credentials.RefreshToken
	provider.ExpiresAtMS = credentials.ExpiresAtMS
	cl.saveProviderConfig(ctx, provider)
	log.Debug().Time("expires_at", time.UnixMilli(provider.ExpiresAtMS)).Msg("Refreshed AI provider credentials")
	return provider, nil
}

func (cl *Client) savedProviderConfig(providerID string) (aiid.ProviderConfig, bool) {
	if cl == nil || cl.Main == nil || cl.UserLogin == nil || providerID == "" {
		return aiid.ProviderConfig{}, false
	}
	return cl.Main.providerForLogin(cl.UserLogin, providerID)
}

func (cl *Client) saveProviderConfig(ctx context.Context, provider aiid.ProviderConfig) {
	if cl == nil || cl.Main == nil || cl.UserLogin == nil || provider.ID == "" {
		return
	}
	meta, ok := cl.UserLogin.Metadata.(*aiid.UserLoginMetadata)
	if !ok || meta == nil {
		return
	}
	if meta.Providers == nil {
		meta.Providers = map[string]aiid.ProviderConfig{}
	}
	meta.Providers[provider.ID] = provider
	_ = cl.UserLogin.Save(ctx)
}

func resolveConfiguredAPIKey(apiKey string) string {
	if envName, ok := strings.CutPrefix(apiKey, "env:"); ok {
		return os.Getenv(strings.TrimSpace(envName))
	}
	return apiKey
}

func isImageModel(model ai.Model) bool {
	return modelHasInput(model, "image")
}

func isAudioModel(model ai.Model) bool {
	return modelHasInput(model, "audio")
}

func modelHasInput(model ai.Model, inputType string) bool {
	for _, input := range model.Input {
		if input == inputType {
			return true
		}
	}
	return false
}
