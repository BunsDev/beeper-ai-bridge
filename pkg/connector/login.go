package connector

import (
	"context"
	"fmt"
	"strings"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
)

const (
	loginFlowDefaultProvider = "beeper"
	loginFlowCustomProvider  = "custom_provider"
	loginStepDefault         = "com.beeper.ai.login.default"
	loginStepCustomProvider  = "com.beeper.ai.login.custom_provider"
	loginStepComplete        = "com.beeper.ai.login.complete"
)

func (c *Connector) GetLoginFlows() []bridgev2.LoginFlow {
	return []bridgev2.LoginFlow{{
		Name:        "Beeper AI",
		Description: "Use the default Beeper AI provider",
		ID:          loginFlowDefaultProvider,
	}, {
		Name:        "Custom provider",
		Description: "Add an OpenAI Responses-compatible provider",
		ID:          loginFlowCustomProvider,
	}}
}

func (c *Connector) CreateLogin(ctx context.Context, user *bridgev2.User, flowID string) (bridgev2.LoginProcess, error) {
	switch flowID {
	case loginFlowDefaultProvider:
		return &DefaultProviderLogin{Main: c, User: user}, nil
	case loginFlowCustomProvider:
		return &CustomProviderLogin{Main: c, User: user}, nil
	default:
		return nil, fmt.Errorf("invalid login flow ID")
	}
}

type DefaultProviderLogin struct {
	Main *Connector
	User *bridgev2.User
}

var _ bridgev2.LoginProcess = (*DefaultProviderLogin)(nil)

func (l *DefaultProviderLogin) Start(ctx context.Context) (*bridgev2.LoginStep, error) {
	login, err := l.Main.EnsureDefaultLogin(ctx, l.User)
	if err != nil {
		return nil, err
	}
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeComplete,
		StepID:       loginStepDefault,
		Instructions: "Beeper AI provider ready",
		CompleteParams: &bridgev2.LoginCompleteParams{
			UserLoginID: login.ID,
			UserLogin:   login,
		},
	}, nil
}

func (l *DefaultProviderLogin) Cancel() {
}

type CustomProviderLogin struct {
	Main *Connector
	User *bridgev2.User
}

var _ bridgev2.LoginProcessUserInput = (*CustomProviderLogin)(nil)

func (l *CustomProviderLogin) Start(ctx context.Context) (*bridgev2.LoginStep, error) {
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeUserInput,
		StepID:       loginStepCustomProvider,
		Instructions: "Enter provider details",
		UserInputParams: &bridgev2.LoginUserInputParams{Fields: []bridgev2.LoginInputDataField{
			{Type: bridgev2.LoginInputFieldTypeUsername, ID: "provider_id", Name: "Provider ID", Description: "Stable ID for this provider"},
			{Type: bridgev2.LoginInputFieldTypeUsername, ID: "display_name", Name: "Display name", Description: "Human-readable provider name"},
			{Type: bridgev2.LoginInputFieldTypeURL, ID: "base_url", Name: "Base URL", Description: "OpenAI-compatible base URL"},
			{Type: bridgev2.LoginInputFieldTypeToken, ID: "api_key", Name: "API key", Description: "Provider API key"},
			{Type: bridgev2.LoginInputFieldTypeUsername, ID: "default_model", Name: "Default model", Description: "Model ID to use by default"},
			{Type: bridgev2.LoginInputFieldTypeUsername, ID: "models", Name: "Models", Description: "Optional comma-separated model IDs"},
		}},
	}, nil
}

func (l *CustomProviderLogin) SubmitUserInput(ctx context.Context, input map[string]string) (*bridgev2.LoginStep, error) {
	providerID := strings.TrimSpace(input["provider_id"])
	if providerID == "" {
		providerID = "custom"
	}
	displayName := strings.TrimSpace(input["display_name"])
	if displayName == "" {
		displayName = providerID
	}
	baseURL := normalizeResponsesBaseURL(strings.TrimSpace(input["base_url"]))
	apiKey := strings.TrimSpace(input["api_key"])
	modelID := strings.TrimSpace(input["default_model"])
	if baseURL == "" || apiKey == "" || modelID == "" {
		return nil, fmt.Errorf("base_url, api_key and default_model are required")
	}
	models := providerModels(strings.TrimSpace(input["models"]), modelID, providerID, baseURL)
	provider := aiid.ProviderConfig{
		ID:           providerID,
		DisplayName:  displayName,
		API:          ai.ApiOpenAIResponses,
		Provider:     ai.Provider(providerID),
		BaseURL:      baseURL,
		APIKey:       apiKey,
		DefaultModel: modelID,
		Models:       models,
		Enabled:      true,
	}
	meta := &aiid.UserLoginMetadata{
		Providers:         map[string]aiid.ProviderConfig{providerID: provider},
		DefaultProviderID: providerID,
		DefaultModelID:    modelID,
	}
	login, err := l.User.NewLogin(ctx, &database.UserLogin{
		ID:         aiid.CustomLoginID(l.User.MXID, providerID),
		RemoteName: displayName,
		Metadata:   meta,
	}, &bridgev2.NewLoginParams{DontReuseExisting: true})
	if err != nil {
		return nil, fmt.Errorf("failed to create provider login: %w", err)
	}
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeComplete,
		StepID:       loginStepComplete,
		Instructions: "Provider added",
		CompleteParams: &bridgev2.LoginCompleteParams{
			UserLoginID: login.ID,
			UserLogin:   login,
		},
	}, nil
}

func (l *CustomProviderLogin) Cancel() {}

func providerModels(modelList string, defaultModel string, providerID string, baseURL string) []ai.Model {
	seen := map[string]bool{}
	modelIDs := []string{defaultModel}
	for _, raw := range strings.FieldsFunc(modelList, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\t' || r == ' '
	}) {
		modelID := strings.TrimSpace(raw)
		if modelID != "" {
			modelIDs = append(modelIDs, modelID)
		}
	}
	models := make([]ai.Model, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		if seen[modelID] {
			continue
		}
		seen[modelID] = true
		models = append(models, ai.Model{
			ID:            modelID,
			Name:          modelID,
			API:           ai.ApiOpenAIResponses,
			Provider:      ai.Provider(providerID),
			BaseURL:       baseURL,
			Input:         []string{"text", "image"},
			ContextWindow: 128000,
			MaxTokens:     32000,
		})
	}
	return models
}
