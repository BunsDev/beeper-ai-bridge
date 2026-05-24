package connector

import (
	"context"
	"testing"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
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
	if config.RoomStateEventType != aiid.RoomConfigType || config.StreamType != aiid.StreamType {
		t.Fatalf("unexpected event types %#v", config)
	}
	if config.DefaultProvider.BaseURL == "" || len(config.DefaultProvider.Models) == 0 {
		t.Fatalf("expected default provider config, got %#v", config.DefaultProvider)
	}
}

func TestConnectorCapabilitiesAdvertiseAISessionCreation(t *testing.T) {
	caps := (&Connector{}).GetCapabilities()
	if _, ok := caps.Provisioning.GroupCreation["ai"]; !ok {
		t.Fatalf("expected AI group creation capability, got %#v", caps.Provisioning.GroupCreation)
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
