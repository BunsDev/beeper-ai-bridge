package connector

import (
	"testing"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/bridgeconfig"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/id"

	"github.com/beeper/ai-bridge/pkg/aiid"
)

func TestConnectorGetNameMatchesDesktopMetadata(t *testing.T) {
	name := (&Connector{}).GetName()

	if name.DisplayName != "AI Chats" {
		t.Fatalf("unexpected display name %q", name.DisplayName)
	}
	if name.NetworkURL != "https://www.beeper.com/ai" {
		t.Fatalf("unexpected network URL %q", name.NetworkURL)
	}
	if string(name.NetworkIcon) != defaultAIAssistantAvatarMXC {
		t.Fatalf("unexpected network icon %q", name.NetworkIcon)
	}
	if name.NetworkID != aiid.NetworkID || name.BeeperBridgeType != aiid.BeeperBridgeType {
		t.Fatalf("unexpected bridge identity: %#v", name)
	}
	if name.DefaultPort != 29344 {
		t.Fatalf("unexpected default port %d", name.DefaultPort)
	}
	if name.DefaultCommandPrefix != "!ai" {
		t.Fatalf("unexpected default command prefix %q", name.DefaultCommandPrefix)
	}
}

func TestConnectorBridgeInfoVersions(t *testing.T) {
	info, caps := (&Connector{}).GetBridgeInfoVersion()
	if info != 1 || caps != 7 {
		t.Fatalf("unexpected bridge info versions info=%d caps=%d", info, caps)
	}
}

func TestDefaultLoginIDFallsBackToPerUserWithoutBridgeBot(t *testing.T) {
	conn := &Connector{}
	userID := id.UserID("@alice:beeper.com")
	if got, want := conn.defaultLoginID(userID), aiid.DefaultLoginID(userID); got != want {
		t.Fatalf("default login ID should be per-user, got %q want %q", got, want)
	}
}

func TestConfiguredLoginUserIDsOnlyIncludesExplicitLoginUsers(t *testing.T) {
	conn := &Connector{Bridge: &bridgev2.Bridge{Config: &bridgeconfig.BridgeConfig{
		Permissions: bridgeconfig.PermissionConfig{
			"*":                         &bridgeconfig.PermissionLevelUser,
			"beeper.com":                &bridgeconfig.PermissionLevelUser,
			"@commands:beeper.com":      &bridgeconfig.PermissionLevelCommands,
			"@alice:beeper.com":         &bridgeconfig.PermissionLevelAdmin,
			"@bob:beeper.com":           &bridgeconfig.PermissionLevelUser,
			"@malformed-without-server": &bridgeconfig.PermissionLevelUser,
		},
	}}}
	userIDs := conn.configuredLoginUserIDs()
	want := []id.UserID{"@alice:beeper.com", "@bob:beeper.com"}
	if len(userIDs) != len(want) {
		t.Fatalf("unexpected configured users %#v", userIDs)
	}
	for i := range want {
		if userIDs[i] != want[i] {
			t.Fatalf("unexpected configured users %#v", userIDs)
		}
	}
}

func TestBeeperLoginStartPromptsForConfigLogin(t *testing.T) {
	step, err := (&BeeperLogin{
		Main: &Connector{},
		User: &bridgev2.User{User: &database.User{MXID: "@alice:beeper.com"}},
	}).Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if step.Type != bridgev2.LoginStepTypeUserInput || step.UserInputParams == nil {
		t.Fatalf("expected user input login step, got %#v", step)
	}
	fields := map[string]string{}
	for _, field := range step.UserInputParams.Fields {
		fields[field.ID] = field.DefaultValue
	}
	if fields["login_id"] != string(aiid.DefaultLoginID(id.UserID("@alice:beeper.com"))) {
		t.Fatalf("unexpected default login id %q", fields["login_id"])
	}
	if fields["display_name"] != "Beeper AI" {
		t.Fatalf("unexpected default display name %q", fields["display_name"])
	}
}

func TestValidateAIChatsLoginID(t *testing.T) {
	for _, loginID := range []networkid.UserLoginID{"work", "work:codex", "beeper.ai"} {
		if err := validateAIChatsLoginID(loginID); err != nil {
			t.Fatalf("expected %q to be valid: %v", loginID, err)
		}
	}
	for _, loginID := range []networkid.UserLoginID{"", "all", "bad id", "bad/id", "bad?id"} {
		if err := validateAIChatsLoginID(loginID); err == nil {
			t.Fatalf("expected %q to be rejected", loginID)
		}
	}
}
