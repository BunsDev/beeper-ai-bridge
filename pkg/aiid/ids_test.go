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
	if got := AssistantUserID(); got != "assistant:ai" {
		t.Fatalf("unexpected assistant ID %q", got)
	}
	providerID, modelID, ok := ParseModelContactID(ModelContactID("beeper/openai", "gpt-5:latest"))
	if !ok || providerID != "beeper/openai" || modelID != "gpt-5:latest" {
		t.Fatalf("model contact ID did not parse: %q %q %v", providerID, modelID, ok)
	}
	providerID, modelID, ok = ParseModelContactID(ModelContactID("beeper", "gpt-5"))
	if !ok || providerID != "beeper" || modelID != "gpt-5" {
		t.Fatalf("model contact ID did not parse: %q %q %v", providerID, modelID, ok)
	}
	providerLogin := ProviderLoginID(loginID, "openai")
	if !strings.HasPrefix(string(providerLogin), "provider:") || !strings.Contains(string(providerLogin), ":openai") {
		t.Fatalf("unexpected provider login ID %q", providerLogin)
	}
}

func TestMetadataJSONRoundTrip(t *testing.T) {
	provider := ProviderConfig{
		ID:           "custom",
		DisplayName:  "Custom",
		API:          ai.ApiOpenAIResponses,
		Provider:     "custom",
		BaseURL:      "https://example.com/v1/responses",
		DefaultModel: "gpt-5",
	}
	meta := &UserLoginMetadata{
		Provider: &provider,
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	var decoded UserLoginMetadata
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Provider == nil || decoded.Provider.BaseURL != "https://example.com/v1/responses" {
		t.Fatalf("provider metadata did not round trip: %#v", decoded.Provider)
	}
	if decoded.Provider.DefaultModel != "gpt-5" {
		t.Fatalf("metadata did not round trip: %#v", decoded)
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
