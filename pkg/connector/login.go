package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

const (
	loginFlowBeeper               = "beeper"
	loginFlowOpenAIResponses      = "openai-responses"
	loginFlowOpenAICompletions    = "openai-completions"
	loginFlowOpenAICodexResponses = "openai-codex-responses"
	loginFlowChatGPTDevice        = "chatgpt-device"
	loginStepBeeper               = "com.beeper.ai.login.beeper"
	loginStepProviderConfig       = "com.beeper.ai.login.provider.config"
	loginStepProviderDefault      = "com.beeper.ai.login.provider.default_model"
	loginStepComplete             = "com.beeper.ai.login.complete"
)

func (c *Connector) GetLoginFlows() []bridgev2.LoginFlow {
	return []bridgev2.LoginFlow{{
		Name:        "Beeper AI",
		Description: "Use the default Beeper AI provider",
		ID:          loginFlowBeeper,
	}, {
		Name:        "OpenAI Responses",
		Description: "Add a provider using the OpenAI Responses API",
		ID:          loginFlowOpenAIResponses,
	}, {
		Name:        "OpenAI Chat Completions",
		Description: "Add a provider using the OpenAI chat completions API",
		ID:          loginFlowOpenAICompletions,
	}, {
		Name:        "OpenAI Codex Responses",
		Description: "Add a provider using the OpenAI Codex Responses API",
		ID:          loginFlowOpenAICodexResponses,
	}, {
		Name:        "ChatGPT",
		Description: "Log in with ChatGPT using a browser device code",
		ID:          loginFlowChatGPTDevice,
	}}
}

func (c *Connector) CreateLogin(ctx context.Context, user *bridgev2.User, flowID string) (bridgev2.LoginProcess, error) {
	switch flowID {
	case loginFlowBeeper:
		return &BeeperLogin{Main: c, User: user}, nil
	case loginFlowOpenAIResponses:
		return &CustomProviderLogin{Main: c, User: user, config: providerLoginConfig{API: ai.ApiOpenAIResponses}}, nil
	case loginFlowOpenAICompletions:
		return &CustomProviderLogin{Main: c, User: user, config: providerLoginConfig{API: ai.ApiOpenAICompletions}}, nil
	case loginFlowOpenAICodexResponses:
		return &CustomProviderLogin{Main: c, User: user, config: providerLoginConfig{API: ai.ApiOpenAICodexResponses}}, nil
	case loginFlowChatGPTDevice:
		return &ChatGPTDeviceLogin{Main: c, User: user}, nil
	default:
		return nil, fmt.Errorf("invalid login flow ID")
	}
}

type BeeperLogin struct {
	Main *Connector
	User *bridgev2.User
}

var _ bridgev2.LoginProcessUserInput = (*BeeperLogin)(nil)

func (l *BeeperLogin) Start(ctx context.Context) (*bridgev2.LoginStep, error) {
	log := providerLoginLog(ctx, loginFlowBeeper, aiid.DefaultProvider)
	log.Debug().Msg("Prompting for AI bridge login config")
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeUserInput,
		StepID:       loginStepBeeper,
		Instructions: "Create an AI bridge login",
		UserInputParams: &bridgev2.LoginUserInputParams{Fields: []bridgev2.LoginInputDataField{
			{Type: bridgev2.LoginInputFieldTypeUsername, ID: "login_id", Name: "Login ID", Description: "Stable ID for this AI configuration", DefaultValue: string(l.Main.defaultLoginID(l.User.MXID))},
			{Type: bridgev2.LoginInputFieldTypeUsername, ID: "display_name", Name: "Display name", Description: "Name shown for this AI configuration", DefaultValue: "Beeper AI"},
		}},
	}, nil
}

func (l *BeeperLogin) SubmitUserInput(ctx context.Context, input map[string]string) (*bridgev2.LoginStep, error) {
	loginID := strings.TrimSpace(input["login_id"])
	displayName := strings.TrimSpace(input["display_name"])
	log := providerLoginLog(ctx, loginFlowBeeper, loginID)
	ctx = log.WithContext(ctx)
	login, err := l.Main.CreateAIChatsLogin(ctx, l.User, networkid.UserLoginID(loginID), displayName)
	if err != nil {
		log.Err(err).Msg("Failed to create AI bridge login")
		return nil, err
	}
	log.Debug().Str("login_id", string(login.ID)).Msg("AI bridge login ready")
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeComplete,
		StepID:       loginStepComplete,
		Instructions: "AI bridge login ready",
		CompleteParams: &bridgev2.LoginCompleteParams{
			UserLoginID: login.ID,
			UserLogin:   login,
		},
	}, nil
}

func (l *BeeperLogin) Cancel() {
}

type CustomProviderLogin struct {
	Main   *Connector
	User   *bridgev2.User
	config providerLoginConfig
}

var _ bridgev2.LoginProcessUserInput = (*CustomProviderLogin)(nil)

func (l *CustomProviderLogin) Start(ctx context.Context) (*bridgev2.LoginStep, error) {
	log := providerLoginLog(ctx, string(l.config.API), "")
	log.Debug().Msg("Prompting for provider login config")
	return l.providerConfigStep(), nil
}

func (l *CustomProviderLogin) SubmitUserInput(ctx context.Context, input map[string]string) (*bridgev2.LoginStep, error) {
	if _, ok := input["default_model"]; ok {
		return l.submitDefaultModel(ctx, input)
	}
	return l.submitProviderConfig(ctx, input)
}

func (l *CustomProviderLogin) providerConfigStep() *bridgev2.LoginStep {
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeUserInput,
		StepID:       loginStepProviderConfig,
		Instructions: "Enter provider connection details",
		UserInputParams: &bridgev2.LoginUserInputParams{Fields: []bridgev2.LoginInputDataField{
			{Type: bridgev2.LoginInputFieldTypeUsername, ID: "provider_id", Name: "ID", Description: "Stable provider ID"},
			{Type: bridgev2.LoginInputFieldTypeURL, ID: "base_url", Name: "Base URL", Description: "OpenAI-compatible API base URL", DefaultValue: defaultBaseURLForAPI(l.config.API)},
			{Type: bridgev2.LoginInputFieldTypeToken, ID: "api_key", Name: "API key", Description: "Provider API key"},
		}},
	}
}

func (l *CustomProviderLogin) submitProviderConfig(ctx context.Context, input map[string]string) (*bridgev2.LoginStep, error) {
	providerID := strings.TrimSpace(input["provider_id"])
	baseURL := normalizeResponsesBaseURL(strings.TrimSpace(input["base_url"]))
	apiKey := strings.TrimSpace(input["api_key"])
	log := providerLoginLog(ctx, string(l.config.API), providerID)
	log = providerLoginURLFields(log, baseURL)
	ctx = log.WithContext(ctx)
	if providerID == "" || baseURL == "" || apiKey == "" {
		err := fmt.Errorf("provider_id, base_url and api_key are required")
		log.Err(err).Msg("Provider login config rejected")
		return nil, err
	}
	models, err := fetchProviderModels(ctx, l.config.API, providerID, baseURL, apiKey)
	if err != nil {
		log.Err(err).Msg("Failed to fetch provider models during login")
		return nil, err
	}
	log.Debug().Int("model_count", len(models)).Msg("Fetched provider models during login")
	l.config.ProviderID = providerID
	l.config.BaseURL = baseURL
	l.config.APIKey = apiKey
	l.config.Models = models
	options := make([]string, 0, len(models))
	for _, model := range models {
		options = append(options, model.ID)
	}
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeUserInput,
		StepID:       loginStepProviderDefault,
		Instructions: "Choose the default model",
		UserInputParams: &bridgev2.LoginUserInputParams{Fields: []bridgev2.LoginInputDataField{
			{Type: bridgev2.LoginInputFieldTypeSelect, ID: "default_model", Name: "Default model", Description: "Model to use by default", DefaultValue: options[0], Options: options},
		}},
	}, nil
}

func (l *CustomProviderLogin) submitDefaultModel(ctx context.Context, input map[string]string) (*bridgev2.LoginStep, error) {
	modelID := strings.TrimSpace(input["default_model"])
	log := providerLoginLog(ctx, string(l.config.API), l.config.ProviderID).
		With().
		Str("model_id", modelID).
		Logger()
	ctx = log.WithContext(ctx)
	if modelID == "" {
		err := fmt.Errorf("default_model is required")
		log.Err(err).Msg("Provider default model rejected")
		return nil, err
	}
	if !providerHasModel(aiid.ProviderConfig{Models: l.config.Models}, modelID) {
		err := fmt.Errorf("model %s was not returned by provider %s", modelID, l.config.ProviderID)
		log.Err(err).Msg("Provider default model rejected")
		return nil, err
	}
	displayName := providerDisplayName(l.config.ProviderID)
	provider := aiid.ProviderConfig{
		ID:           l.config.ProviderID,
		DisplayName:  displayName,
		API:          l.config.API,
		Provider:     configuredModelProvider(l.config.ProviderID, ai.Provider(l.config.ProviderID)),
		BaseURL:      l.config.BaseURL,
		APIKey:       l.config.APIKey,
		DefaultModel: modelID,
		Models:       l.config.Models,
	}
	login, err := l.Main.EnsureAIChatsLogin(ctx, l.User)
	if err != nil {
		err = fmt.Errorf("failed to load AI bridge login: %w", err)
		log.Err(err).Msg("Failed to load AI bridge login")
		return nil, err
	}
	err = l.Main.SaveProviderConfig(ctx, login, provider)
	if err != nil {
		err = fmt.Errorf("failed to save provider: %w", err)
		log.Err(err).Msg("Failed to save provider")
		return nil, err
	}
	log.Debug().Str("login_id", string(login.ID)).Msg("Provider added")
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

type providerLoginConfig struct {
	API        ai.Api
	ProviderID string
	BaseURL    string
	APIKey     string
	Models     []ai.Model
}

func supportedProviderLoginAPI(api ai.Api) bool {
	switch api {
	case ai.ApiOpenAIResponses, ai.ApiOpenAICompletions, ai.ApiOpenAICodexResponses:
		return true
	default:
		return false
	}
}

func defaultBaseURLForAPI(api ai.Api) string {
	return "https://api.openai.com/v1"
}

func providerDisplayName(providerID string) string {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return "Provider"
	}
	known := map[string]string{
		"openai":     "OpenAI",
		"openrouter": "OpenRouter",
		"lmstudio":   "LM Studio",
	}
	if name, ok := known[strings.ToLower(providerID)]; ok {
		return name
	}
	parts := strings.FieldsFunc(providerID, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	})
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func fetchProviderModels(ctx context.Context, api ai.Api, providerID string, baseURL string, apiKey string) ([]ai.Model, error) {
	if !supportedProviderLoginAPI(api) {
		return nil, fmt.Errorf("unsupported API type %s", api)
	}
	modelURL, err := url.JoinPath(baseURL, "models")
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelURL, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+resolveConfiguredAPIKey(apiKey))
	}
	client := &http.Client{Timeout: 20 * time.Second}
	log := zerolog.Ctx(ctx).With().
		Str("action", "ai_provider_models_http").
		Str("provider_id", providerID).
		Str("api", string(api)).
		Str("method", http.MethodGet).
		Str("url", modelURL).
		Logger()
	if parsed, parseErr := url.Parse(modelURL); parseErr == nil {
		log = log.With().Str("host", parsed.Host).Str("path", parsed.EscapedPath()).Logger()
	}
	ctx = log.WithContext(ctx)
	log.Trace().Msg("Sending provider models HTTP request")
	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		log.Err(err).Dur("duration", time.Since(started)).Msg("Provider models HTTP request failed")
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()
	logEvent := log.Debug()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logEvent = log.Error()
	}
	logEvent.Dur("duration", time.Since(started)).
		Int("status_code", resp.StatusCode).
		Str("status", resp.Status).
		Int64("response_content_length", resp.ContentLength).
		Str("response_content_type", resp.Header.Get("Content-Type")).
		Msg("Received provider models HTTP response")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("failed to fetch models: provider returned HTTP %d", resp.StatusCode)
	}
	var body aiServicesModelListResponse
	if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("failed to parse models response: %w", err)
	}
	models := make([]ai.Model, 0, len(body.Data))
	seen := map[string]bool{}
	provider, inferredAPI := inferProviderRoute(providerID, baseURL)
	if api != "" {
		inferredAPI = api
	}
	providerConfig := aiid.ProviderConfig{
		ID:       providerID,
		API:      inferredAPI,
		Provider: configuredModelProvider(providerID, provider),
		BaseURL:  baseURL,
	}
	for _, item := range body.Data {
		modelID := strings.TrimSpace(item.ID)
		if modelID == "" || seen[modelID] {
			continue
		}
		seen[modelID] = true
		model := ai.Model{
			ID:            modelID,
			Name:          item.Name,
			API:           inferredAPI,
			Provider:      providerConfig.Provider,
			BaseURL:       baseURL,
			Reasoning:     item.reasoning(),
			Input:         item.inputModalities(),
			ContextWindow: item.contextWindow(),
			MaxTokens:     item.maxTokens(),
		}
		model = item.applyProviderRoute(model, providerConfig)
		models = append(models, normalizeProviderModel(model, providerConfig))
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("provider returned no models")
	}
	log.Debug().Int("model_count", len(models)).Msg("Parsed provider models")
	return models, nil
}

func customProviderConfig(providerID string, displayName string, baseURL string, apiKey string, defaultModel string, modelList string) aiid.ProviderConfig {
	provider, api := inferProviderRoute(providerID, baseURL)
	modelIDs := providerModelIDs(modelList, defaultModel)
	return aiid.ProviderConfig{
		ID:           providerID,
		DisplayName:  displayName,
		API:          api,
		Provider:     provider,
		BaseURL:      baseURL,
		APIKey:       apiKey,
		DefaultModel: defaultModel,
		Models:       providerModelsFromIDs(modelIDs, providerID, provider, api, baseURL),
	}
}

func inferProviderRoute(providerID string, baseURL string) (ai.Provider, ai.Api) {
	providerID = strings.ToLower(providerID)
	baseURL = strings.ToLower(baseURL)
	if providerID == string(ai.ProviderOpenRouter) || strings.Contains(baseURL, "openrouter.ai") {
		return ai.ProviderOpenRouter, ai.ApiOpenAICompletions
	}
	return ai.ProviderOpenAI, ai.ApiOpenAIResponses
}

func providerModels(modelList string, defaultModel string, providerID string, baseURL string) []ai.Model {
	provider, api := inferProviderRoute(providerID, baseURL)
	return providerModelsFromIDs(providerModelIDs(modelList, defaultModel), providerID, provider, api, baseURL)
}

func providerModelIDs(modelList string, defaultModel string) []string {
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
	out := make([]string, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		if seen[modelID] {
			continue
		}
		seen[modelID] = true
		out = append(out, modelID)
	}
	return out
}

func providerModelsFromIDs(modelIDs []string, providerID string, provider ai.Provider, api ai.Api, baseURL string) []ai.Model {
	models := make([]ai.Model, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		models = append(models, ai.Model{
			ID:            modelID,
			Name:          modelID,
			API:           api,
			Provider:      configuredModelProvider(providerID, provider),
			BaseURL:       baseURL,
			Input:         []string{"text", "image"},
			ContextWindow: 128000,
			MaxTokens:     32000,
		})
	}
	return models
}

func configuredModelProvider(providerID string, provider ai.Provider) ai.Provider {
	if providerID != string(ai.ProviderOpenAI) && providerID != string(ai.ProviderOpenRouter) {
		return ai.Provider(providerID)
	}
	return provider
}

func providerLoginLog(ctx context.Context, flowID string, providerID string) zerolog.Logger {
	logCtx := zerolog.Ctx(ctx).With().
		Str("action", "ai_provider_login").
		Str("flow_id", flowID)
	if providerID != "" {
		logCtx = logCtx.Str("provider_id", providerID)
	}
	return logCtx.Logger()
}

func providerLoginURLFields(log zerolog.Logger, rawURL string) zerolog.Logger {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return log
	}
	return log.With().
		Str("base_url_host", parsed.Host).
		Str("base_url_path", parsed.EscapedPath()).
		Logger()
}
