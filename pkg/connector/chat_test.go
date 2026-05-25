package connector

import (
	"context"
	"testing"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func TestCreateGroupMapsMatrixRoomToAISessionPortal(t *testing.T) {
	client := &Client{UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{ID: networkid.UserLoginID("login")}}}
	response, err := client.CreateGroup(context.Background(), &bridgev2.GroupCreateParams{
		Type:   "ai",
		RoomID: id.RoomID("!room:example.com"),
		Name:   &event.RoomNameEventContent{Name: "Work AI"},
	})
	if err != nil {
		t.Fatal(err)
	}
	expected := aiid.PortalKey(id.RoomID("!room:example.com"), networkid.UserLoginID("login"))
	if response.PortalKey != expected {
		t.Fatalf("unexpected portal key %#v", response.PortalKey)
	}
	if response.PortalInfo == nil || response.PortalInfo.Name == nil || *response.PortalInfo.Name != "Work AI" {
		t.Fatalf("unexpected portal info %#v", response.PortalInfo)
	}
	if response.PortalInfo.Type == nil || *response.PortalInfo.Type != database.RoomTypeDM {
		t.Fatalf("expected AI rooms to be DMs, got %#v", response.PortalInfo.Type)
	}
}

func TestCreateGroupRequiresExistingMatrixRoom(t *testing.T) {
	client := &Client{UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{ID: networkid.UserLoginID("login")}}}
	if _, err := client.CreateGroup(context.Background(), &bridgev2.GroupCreateParams{Type: "ai"}); err == nil {
		t.Fatalf("expected roomless AI group creation to fail")
	}
}

func TestGetChatInfoUsesDefaultModelTitleAndDMType(t *testing.T) {
	client := &Client{UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{
		ID: networkid.UserLoginID("login"),
		Metadata: &aiid.UserLoginMetadata{Providers: map[string]aiid.ProviderConfig{
			"local": {
				ID:          "local",
				DisplayName: "Local",
				API:         ai.ApiOpenAIResponses,
				Provider:    "local",
				Models:      []ai.Model{{ID: "model-a", Name: "Model Alpha"}},
				Enabled:     true,
			},
		}},
	}}}
	portal := &bridgev2.Portal{Portal: &database.Portal{Metadata: &aiid.PortalMetadata{
		SelectedProviderID: "local",
		SelectedModelID:    "model-a",
	}}}

	info, err := client.GetChatInfo(context.Background(), portal)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name == nil || *info.Name != "New AI Chat with Model Alpha" {
		t.Fatalf("unexpected default title %#v", info.Name)
	}
	if info.Type == nil || *info.Type != database.RoomTypeDM {
		t.Fatalf("expected AI chat info to be DM, got %#v", info.Type)
	}
}
