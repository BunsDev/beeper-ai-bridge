package connector

import (
	"context"
	"testing"

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
}

func TestCreateGroupRequiresExistingMatrixRoom(t *testing.T) {
	client := &Client{UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{ID: networkid.UserLoginID("login")}}}
	if _, err := client.CreateGroup(context.Background(), &bridgev2.GroupCreateParams{Type: "ai"}); err == nil {
		t.Fatalf("expected roomless AI group creation to fail")
	}
}
