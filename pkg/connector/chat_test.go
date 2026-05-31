package connector

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/beeper/ai-bridge/pkg/agent/harness/session"
	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aidb"
	"github.com/beeper/ai-bridge/pkg/aiid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog"
	"go.mau.fi/util/dbutil"
	"go.mau.fi/util/jsontime"
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
		Topic:  &event.TopicEventContent{Topic: "Project notes"},
		Disappear: &event.BeeperDisappearingTimer{
			Type:  event.DisappearingTypeAfterRead,
			Timer: jsontime.MS(time.Hour),
		},
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
	if response.PortalInfo.Topic == nil || *response.PortalInfo.Topic != "Project notes" {
		t.Fatalf("unexpected portal topic %#v", response.PortalInfo.Topic)
	}
	if response.PortalInfo.Type == nil || *response.PortalInfo.Type != database.RoomTypeDM {
		t.Fatalf("expected AI rooms to be DMs, got %#v", response.PortalInfo.Type)
	}
	if response.PortalInfo.Disappear == nil || response.PortalInfo.Disappear.Type != event.DisappearingTypeAfterRead || response.PortalInfo.Disappear.Timer != time.Hour {
		t.Fatalf("unexpected disappearing setting %#v", response.PortalInfo.Disappear)
	}
	if response.PortalInfo.Avatar == nil || response.PortalInfo.Avatar.MXC != id.ContentURIString(defaultAIAssistantAvatarMXC) {
		t.Fatalf("expected default AI room avatar, got %#v", response.PortalInfo.Avatar)
	}
	if !response.PortalInfo.ExcludeChangesFromTimeline {
		t.Fatalf("expected synthetic AI room metadata changes to be excluded from timeline")
	}
}

func TestCreateGroupRequiresExistingMatrixRoom(t *testing.T) {
	client := &Client{UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{ID: networkid.UserLoginID("login")}}}
	if _, err := client.CreateGroup(context.Background(), &bridgev2.GroupCreateParams{Type: "ai"}); err == nil {
		t.Fatalf("expected roomless AI group creation to fail")
	}
}

func TestGetChatInfoUsesDefaultTitleAndDMType(t *testing.T) {
	client := &Client{UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{
		ID:       networkid.UserLoginID("login"),
		Metadata: &aiid.UserLoginMetadata{},
	}}}
	portal := &bridgev2.Portal{Portal: &database.Portal{Metadata: &aiid.PortalMetadata{}}}

	info, err := client.GetChatInfo(context.Background(), portal)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name == nil || *info.Name != "New AI Chat" {
		t.Fatalf("unexpected default title %#v", info.Name)
	}
	if info.Type == nil || *info.Type != database.RoomTypeDM {
		t.Fatalf("expected AI chat info to be DM, got %#v", info.Type)
	}
	if info.Avatar == nil || info.Avatar.MXC != id.ContentURIString(defaultAIAssistantAvatarMXC) {
		t.Fatalf("expected default AI room avatar, got %#v", info.Avatar)
	}
	if !info.ExcludeChangesFromTimeline {
		t.Fatalf("expected synthetic AI room metadata changes to be excluded from timeline")
	}
}

func TestGetChatInfoIncludesStoredRoomInfo(t *testing.T) {
	client := &Client{UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{
		ID:       networkid.UserLoginID("login"),
		Metadata: &aiid.UserLoginMetadata{},
	}}}
	portal := &bridgev2.Portal{Portal: &database.Portal{
		Name:     "Stored AI Title",
		NameSet:  true,
		Topic:    "Stored topic",
		TopicSet: true,
		Disappear: database.DisappearingSetting{
			Type:  event.DisappearingTypeAfterSend,
			Timer: time.Hour,
		},
		Metadata: &aiid.PortalMetadata{},
	}}

	info, err := client.GetChatInfo(context.Background(), portal)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name == nil || *info.Name != "Stored AI Title" {
		t.Fatalf("unexpected stored name %#v", info.Name)
	}
	if info.Topic == nil || *info.Topic != "Stored topic" {
		t.Fatalf("unexpected stored topic %#v", info.Topic)
	}
	if info.Disappear == nil || info.Disappear.Type != event.DisappearingTypeAfterSend || info.Disappear.Timer != time.Hour {
		t.Fatalf("unexpected disappearing setting %#v", info.Disappear)
	}
	if !info.ExcludeChangesFromTimeline {
		t.Fatalf("expected synthetic AI room metadata changes to be excluded from timeline")
	}
}

func TestNetworkCapabilitiesEnableDisappearingMessages(t *testing.T) {
	caps := (&Connector{}).GetCapabilities()
	if caps == nil || !caps.DisappearingMessages {
		t.Fatalf("expected disappearing message loop to be enabled, got %#v", caps)
	}
}

func TestRoomCapabilitiesEnableDeletion(t *testing.T) {
	caps := roomFeaturesForModel(ai.Model{}, true)
	if caps.Delete != event.CapLevelFullySupported {
		t.Fatalf("expected message delete to be fully supported, got %d", caps.Delete)
	}
	if !caps.DeleteChat {
		t.Fatalf("expected delete chat to be supported")
	}
}

func TestActiveRunMatchesOnlyCurrentRedactionTargets(t *testing.T) {
	run := &activeAIRun{
		pending: []*pendingAIMessage{{
			txnID: networkid.TransactionID("$pending"),
		}},
		consumed: []*pendingAIMessage{{
			metadata: &aiid.MessageMetadata{SessionEntryID: "user-entry"},
		}},
		streams: []*assistantStreamState{{
			messageID: networkid.MessageID("assistant:active"),
		}},
		last: &assistantStreamState{
			messageID: networkid.MessageID("assistant:finished"),
		},
	}

	for _, messageID := range []networkid.MessageID{
		"pending:$pending",
		aiid.UserMessageID("user-entry"),
		"assistant:active",
	} {
		if !run.matchesRedactionTarget(&database.Message{ID: messageID}) {
			t.Fatalf("expected active run to match redaction target %q", messageID)
		}
	}
	for _, messageID := range []networkid.MessageID{
		"assistant:finished",
		"assistant:historical",
		"user:",
	} {
		if run.matchesRedactionTarget(&database.Message{ID: messageID}) {
			t.Fatalf("did not expect active run to match redaction target %q", messageID)
		}
	}
	if run.matchesRedactionTarget(nil) {
		t.Fatalf("nil target should not match")
	}
}

func TestHandleMatrixRoomNameUpdatesPortalName(t *testing.T) {
	ctx := context.Background()
	rawDB, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := dbutil.NewWithDB(rawDB, "sqlite3")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	loginID := networkid.UserLoginID("login")
	store := aidb.NewStore(db, networkid.BridgeID("bridge"), dbutil.ZeroLogger(zerolog.Nop()))
	if err := store.Upgrade(ctx); err != nil {
		t.Fatal(err)
	}
	agentSession, err := store.CreateSession(ctx, loginID, session.SQLiteSessionCreateOptions{ID: "session-1"})
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{
		Main:      &Connector{Store: store},
		UserLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{ID: loginID}},
	}
	portal := &bridgev2.Portal{Portal: &database.Portal{Metadata: &aiid.PortalMetadata{SessionID: "session-1", AutoTitlePending: true}}}

	ok, err := client.HandleMatrixRoomName(ctx, &bridgev2.MatrixRoomName{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.RoomNameEventContent]{
			Portal:  portal,
			Content: &event.RoomNameEventContent{Name: "Manual AI Title"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("expected room name change to be handled")
	}
	if portal.Name != "Manual AI Title" || !portal.NameSet {
		t.Fatalf("portal name not updated: name=%q set=%v", portal.Name, portal.NameSet)
	}
	sessionName, err := agentSession.GetSessionName(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sessionName == nil || *sessionName != "Manual AI Title" {
		t.Fatalf("session name not appended: %#v", sessionName)
	}
	if portalMetadata(portal).AutoTitlePending {
		t.Fatalf("manual room name should clear pending auto-title metadata")
	}
}
