package connector

import (
	"context"
	"testing"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/event"
)

func TestModelForProviderConstructsCustomModel(t *testing.T) {
	conn := &Connector{}
	provider := aiid.ProviderConfig{
		ID:       "local",
		API:      ai.ApiOpenAIResponses,
		Provider: ai.Provider("local"),
		BaseURL:  "https://local.test/v1/responses",
	}
	model := conn.ModelForProvider(provider, "local-model")
	if model.ID != "local-model" || model.Provider != "local" || model.API != ai.ApiOpenAIResponses {
		t.Fatalf("unexpected model %#v", model)
	}
	if model.BaseURL != "https://local.test/v1" {
		t.Fatalf("unexpected model base URL %q", model.BaseURL)
	}
	if !isImageModel(model) {
		t.Fatalf("expected custom provider fallback model to support image input")
	}
}

func TestDefaultProviderAuthUsesAppServiceToken(t *testing.T) {
	client := &Client{Main: &Connector{AppServiceToken: "as-token"}}
	auth, err := client.authForProvider(aiid.ProviderConfig{ID: aiid.DefaultProvider})(context.Background(), ai.Model{})
	if err != nil {
		t.Fatal(err)
	}
	if auth.APIKey != "as-token" {
		t.Fatalf("expected appservice token, got %q", auth.APIKey)
	}
}

func TestCustomProviderAuthUsesProviderKeyAndHeaders(t *testing.T) {
	client := &Client{Main: &Connector{}}
	auth, err := client.authForProvider(aiid.ProviderConfig{
		ID:      "custom",
		APIKey:  "custom-key",
		Headers: map[string]string{"X-Test": "ok"},
	})(context.Background(), ai.Model{})
	if err != nil {
		t.Fatal(err)
	}
	if auth.APIKey != "custom-key" || auth.Headers["X-Test"] != "ok" {
		t.Fatalf("unexpected auth %#v", auth)
	}
}

func TestConfigDefaults(t *testing.T) {
	config := Config{}
	config.ApplyDefaults()
	if config.BeeperEnvTLD != "beeper.com" {
		t.Fatalf("unexpected env tld %q", config.BeeperEnvTLD)
	}
	if config.StreamType != aiid.StreamType {
		t.Fatalf("unexpected stream type %#v", config)
	}
	if config.DefaultProvider.BaseURL == "" || len(config.DefaultProvider.Models) == 0 {
		t.Fatalf("expected default provider config, got %#v", config.DefaultProvider)
	}
	if config.DefaultSystemPrompt == "" || config.DefaultReasoningLevel != "off" {
		t.Fatalf("expected chat defaults, got %#v", config)
	}
	if config.Fetch.TimeoutMS == 0 || config.Fetch.MaxBytes == 0 || config.Fetch.MaxChars == 0 {
		t.Fatalf("expected fetch defaults, got %#v", config.Fetch)
	}
}

func TestConnectorCapabilitiesAdvertiseAISessionCreation(t *testing.T) {
	conn := &Connector{}
	caps := conn.GetCapabilities()
	if _, ok := caps.Provisioning.GroupCreation["ai"]; !ok {
		t.Fatalf("expected AI group creation capability, got %#v", caps.Provisioning.GroupCreation)
	}
	if !caps.Provisioning.ResolveIdentifier.ContactList || !caps.Provisioning.ResolveIdentifier.Search || !caps.Provisioning.ResolveIdentifier.CreateDM {
		t.Fatalf("expected contact list/search/create DM capabilities, got %#v", caps.Provisioning.ResolveIdentifier)
	}
	_, capVersion := conn.GetBridgeInfoVersion()
	if capVersion < 3 {
		t.Fatalf("capability version must bump when provisioning capabilities change, got %d", capVersion)
	}
}

func TestRoomFeaturesDisableReactions(t *testing.T) {
	client := &Client{}
	caps := client.GetCapabilities(context.Background(), nil)
	if caps.ID != "" {
		t.Fatalf("expected room features to use content-derived ID, got %q", caps.ID)
	}
	if caps.GetID() == "" {
		t.Fatalf("expected room features to expose a dedupe ID")
	}
	if caps.Reaction != event.CapLevelUnsupported {
		t.Fatalf("expected reactions to be unsupported, got %d", caps.Reaction)
	}
	if !caps.TypingNotifications {
		t.Fatalf("expected assistant typing notifications")
	}
	for _, stateType := range []string{aiid.RoomToolsType, aiid.RoomModelType, aiid.RoomPromptType} {
		if caps.State[stateType] == nil || caps.State[stateType].Level != event.CapLevelFullySupported {
			t.Fatalf("expected %s state support, got %#v", stateType, caps.State[stateType])
		}
	}
}

func TestProviderModelsParsesOptionalModelList(t *testing.T) {
	models := providerModels("gpt-5-mini, gpt-5\nlocal", "gpt-5", "custom", "https://example.test/v1")
	if len(models) != 3 {
		t.Fatalf("expected default plus two unique models, got %#v", models)
	}
	if models[0].ID != "gpt-5" || models[1].ID != "gpt-5-mini" || models[2].ID != "local" {
		t.Fatalf("unexpected model order %#v", models)
	}
}

func TestBuildProviderFromCommandArgs(t *testing.T) {
	provider, err := buildProviderFromCommandArgs([]string{"local", "https://example.test/v1/responses", "key", "model-a", "model-b", "model-c"})
	if err != nil {
		t.Fatal(err)
	}
	if provider.ID != "local" || provider.APIKey != "key" || provider.DefaultModel != "model-a" {
		t.Fatalf("unexpected provider %#v", provider)
	}
	if provider.BaseURL != "https://example.test/v1" {
		t.Fatalf("unexpected base URL %q", provider.BaseURL)
	}
	if len(provider.Models) != 3 || provider.Models[1].ID != "model-b" || provider.Models[2].ID != "model-c" {
		t.Fatalf("unexpected models %#v", provider.Models)
	}
}

func TestResolveProviderValidatesExplicitModelList(t *testing.T) {
	conn := &Connector{}
	login := &bridgev2.UserLogin{UserLogin: &database.UserLogin{
		ID: "login",
		Metadata: &aiid.UserLoginMetadata{
			Providers: map[string]aiid.ProviderConfig{
				"custom": {
					ID:           "custom",
					Provider:     "custom",
					API:          ai.ApiOpenAIResponses,
					DefaultModel: "allowed",
					Models:       []ai.Model{{ID: "allowed", Provider: "custom", API: ai.ApiOpenAIResponses}},
					Enabled:      true,
				},
			},
			DefaultProviderID: "custom",
		},
	}}
	if _, _, err := conn.ResolveProvider(context.Background(), login, RoomConfig{ModelID: "missing"}); err == nil {
		t.Fatalf("expected missing model to fail")
	}
	_, modelID, err := conn.ResolveProvider(context.Background(), login, RoomConfig{ModelID: "allowed"})
	if err != nil {
		t.Fatal(err)
	}
	if modelID != "allowed" {
		t.Fatalf("unexpected model ID %q", modelID)
	}
}

func TestValidateReasoningLevelRejectsUnsupportedPair(t *testing.T) {
	client := &Client{Main: &Connector{Config: Config{DefaultReasoningLevel: "off"}}}
	model := ai.Model{ID: "plain", Input: []string{"text"}}
	if err := client.validateReasoningLevel(model, RoomConfig{ThinkingLevel: "high"}); err == nil {
		t.Fatalf("expected unsupported reasoning level to fail")
	}
	if err := client.validateReasoningLevel(model, RoomConfig{ThinkingLevel: "off"}); err != nil {
		t.Fatalf("expected off reasoning to be accepted: %v", err)
	}
}
