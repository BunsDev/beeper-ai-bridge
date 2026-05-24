package connector

import (
	"context"
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
	providerID, modelID, ok := aiid.ParseAssistantUserID(contacts[0].UserID)
	if !ok || providerID != "local" || modelID != "model-a" {
		t.Fatalf("unexpected ghost ID %q parsed as %q %q", contacts[0].UserID, providerID, modelID)
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
