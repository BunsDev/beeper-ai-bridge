package aiid

import (
	"encoding/json"
	"strings"
	"testing"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"maunium.net/go/mautrix/id"
)

func TestPortalAndAssistantIDsAreStable(t *testing.T) {
	loginID := DefaultLoginID(id.UserID("@alice:example.com"))
	portalKey := PortalKey(id.RoomID("!room:example.com"), loginID)
	if !strings.HasPrefix(string(loginID), "default:") {
		t.Fatalf("expected default login prefix, got %q", loginID)
	}
	if !strings.HasPrefix(string(portalKey.ID), "mxroom:") {
		t.Fatalf("expected mxroom portal prefix, got %q", portalKey.ID)
	}
	if portalKey.Receiver != loginID {
		t.Fatalf("expected receiver %q, got %q", loginID, portalKey.Receiver)
	}
	if got := AssistantUserID("beeper/openai", "gpt-5:latest"); got != "assistant:beeper_openai:gpt-5_latest" {
		t.Fatalf("unexpected assistant ID %q", got)
	}
	providerID, modelID, ok := ParseAssistantUserID(AssistantUserID("beeper", "gpt-5"))
	if !ok || providerID != "beeper" || modelID != "gpt-5" {
		t.Fatalf("assistant ID did not parse: %q %q %v", providerID, modelID, ok)
	}
	aliceCustom := CustomLoginID(id.UserID("@alice:example.com"), "openai")
	bobCustom := CustomLoginID(id.UserID("@bob:example.com"), "openai")
	if aliceCustom == bobCustom {
		t.Fatalf("custom login IDs must be scoped per Matrix user")
	}
}

func TestMetadataJSONRoundTrip(t *testing.T) {
	meta := &UserLoginMetadata{
		SyntheticDefault: true,
		Providers: map[string]ProviderConfig{
			DefaultProvider: {
				ID:           DefaultProvider,
				DisplayName:  "Beeper AI",
				API:          ai.ApiOpenAIResponses,
				Provider:     ai.Provider(DefaultProvider),
				BaseURL:      "https://example.com/v1/responses",
				DefaultModel: "gpt-5",
				Enabled:      true,
			},
		},
		DefaultProviderID: DefaultProvider,
		DefaultModelID:    "gpt-5",
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	var decoded UserLoginMetadata
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.SyntheticDefault || decoded.DefaultModelID != "gpt-5" {
		t.Fatalf("metadata did not round trip: %#v", decoded)
	}
	if decoded.Providers[DefaultProvider].BaseURL != "https://example.com/v1/responses" {
		t.Fatalf("provider metadata did not round trip: %#v", decoded.Providers[DefaultProvider])
	}
}

func TestMediaIDForEncodesMetadata(t *testing.T) {
	metadata := MediaMetadata{
		LoginID:      "login",
		ProviderID:   "provider",
		SessionID:    "session",
		EntryID:      "entry",
		ContentIndex: 1,
		MimeType:     "image/png",
	}
	mediaID, err := MediaIDFor(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(mediaID), "ai:") {
		t.Fatalf("expected encoded AI media ID, got %q", mediaID)
	}
	decoded, err := ParseMediaID(mediaID)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != metadata {
		t.Fatalf("metadata did not round trip: %#v", decoded)
	}
}
