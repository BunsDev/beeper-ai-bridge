package connector

import (
	"context"
	"strings"
	"testing"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
)

func TestModelContactsExposeConfiguredModels(t *testing.T) {
	client := &Client{UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{
		ID: "login",
		Metadata: &aiid.UserLoginMetadata{Providers: map[string]aiid.ProviderConfig{
			"local": {
				ID:          "local",
				DisplayName: "Local",
				Provider:    "local",
				API:         ai.ApiOpenAIResponses,
				Models:      []ai.Model{{ID: "model-a"}, {ID: "model-b", Name: "Model Bee"}},
				Enabled:     true,
			},
		}},
	}}}
	contacts, err := client.GetContactList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(contacts) != 2 {
		t.Fatalf("expected two contacts, got %#v", contacts)
	}
	if contacts[1].UserInfo == nil || contacts[1].UserInfo.Name == nil || *contacts[1].UserInfo.Name != "Model Bee" {
		t.Fatalf("unexpected contact info %#v", contacts[1])
	}
	providerID, modelID, ok := aiid.ParseModelContactID(contacts[0].UserID)
	if !ok || providerID != "local" || modelID != "model-a" {
		t.Fatalf("unexpected model contact ID %q parsed as %q %q", contacts[0].UserID, providerID, modelID)
	}
}

func TestNewAIChatPortalKeyCreatesFreshChatPortal(t *testing.T) {
	first := newAIChatPortalKey("login")
	second := newAIChatPortalKey("login")
	if first.Receiver != "login" || second.Receiver != "login" {
		t.Fatalf("unexpected receiver: %#v %#v", first, second)
	}
	if first.ID == second.ID {
		t.Fatalf("expected fresh portal IDs, got %q twice", first.ID)
	}
	if !strings.HasPrefix(string(first.ID), "chat:") || !strings.HasPrefix(string(second.ID), "chat:") {
		t.Fatalf("expected chat portal IDs, got %#v %#v", first, second)
	}
}

func TestSearchUsersFiltersModelContacts(t *testing.T) {
	client := &Client{UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{
		ID: "login",
		Metadata: &aiid.UserLoginMetadata{Providers: map[string]aiid.ProviderConfig{
			"local": {
				ID:      "local",
				Models:  []ai.Model{{ID: "small"}, {ID: "large"}},
				Enabled: true,
			},
		}},
	}}}
	results, err := client.SearchUsers(context.Background(), "large")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %#v", results)
	}
}

func TestContactListIncludesEnabledConfiguredProviders(t *testing.T) {
	conn := &Connector{Config: Config{
		DefaultProvider: DefaultProviderConfig{
			BaseURL:       "https://ai-proxy.beeper.com/v1/responses",
			Provider:      ai.ProviderOpenAI,
			API:           ai.ApiOpenAIResponses,
			DefaultModel:  "gpt-5.5",
			AllowedModels: []string{"gpt-5.5"},
		},
		Providers: map[string]aiid.ProviderConfig{
			"openai": {
				ID:            "openai",
				DisplayName:   "OpenAI",
				Provider:      ai.ProviderOpenAI,
				API:           ai.ApiOpenAIResponses,
				BaseURL:       "https://api.openai.com/v1",
				DefaultModel:  "gpt-5.5",
				AllowedModels: []string{"gpt-5.5"},
				Enabled:       true,
			},
			"openrouter": {
				ID:            "openrouter",
				DisplayName:   "OpenRouter",
				Provider:      ai.ProviderOpenRouter,
				API:           ai.ApiOpenAICompletions,
				BaseURL:       "https://openrouter.ai/api/v1",
				DefaultModel:  "anthropic/claude-sonnet-4.5",
				AllowedModels: []string{"anthropic/claude-sonnet-4.5"},
				Enabled:       true,
			},
			"disabled": {
				ID:            "disabled",
				DisplayName:   "Disabled",
				Provider:      ai.ProviderOpenAI,
				API:           ai.ApiOpenAIResponses,
				DefaultModel:  "gpt-5",
				AllowedModels: []string{"gpt-5"},
				Enabled:       false,
			},
		},
	}}
	client := &Client{
		Main: conn,
		UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{
			ID: "login",
			Metadata: &aiid.UserLoginMetadata{
				SyntheticDefault: true,
				Providers: map[string]aiid.ProviderConfig{
					"custom": {
						ID:            "custom",
						DisplayName:   "Custom",
						Provider:      "custom",
						API:           ai.ApiOpenAIResponses,
						BaseURL:       "https://custom.test/v1",
						DefaultModel:  "custom-model",
						AllowedModels: []string{"custom-model"},
						Enabled:       true,
					},
				},
			},
		}},
	}

	contacts, err := client.GetContactList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, contact := range contacts {
		for _, identifier := range contact.UserInfo.Identifiers {
			got[identifier] = true
		}
	}
	for _, want := range []string{
		"beeper/gpt-5.5",
		"openai/gpt-5.5",
		"openrouter/anthropic/claude-sonnet-4.5",
		"custom/custom-model",
	} {
		if !got[want] {
			t.Fatalf("expected contact %s in %#v", want, got)
		}
	}
	if got["disabled/gpt-5"] {
		t.Fatalf("disabled provider leaked into contact list: %#v", got)
	}
}
