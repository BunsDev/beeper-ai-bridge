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
	if got := ModelContactID(DefaultProvider, "openai/gpt-5.5"); got != "model:openai=2fgpt-5.5" {
		t.Fatalf("unexpected default model contact ID %q", got)
	}
	if got := ModelContactID("openrouter", "openai/gpt-5.5"); got != "model:openrouter:openai=2fgpt-5.5" {
		t.Fatalf("unexpected custom model contact ID %q", got)
	}
	providerID, modelID, ok := ParseModelContactID(ModelContactID("openrouter", "openai/gpt-5.5"))
	if !ok || providerID != "openrouter" || modelID != "openai/gpt-5.5" {
		t.Fatalf("model contact ID did not parse: %q %q %v", providerID, modelID, ok)
	}
	providerID, modelID, ok = ParseModelContactID(ModelContactID(DefaultProvider, "gpt-5+preview"))
	if !ok || providerID != DefaultProvider || modelID != "gpt-5+preview" {
		t.Fatalf("default model contact ID did not parse: %q %q %v", providerID, modelID, ok)
	}
	providerID, modelID, ok = ParseModelContactID(ModelContactID("custom", "gpt-5+preview"))
	if !ok || providerID != "custom" || modelID != "gpt-5+preview" {
		t.Fatalf("custom model contact ID did not preserve model separator: %q %q %v", providerID, modelID, ok)
	}
	providerID, modelID, ok = ParseModelContactID(ModelContactID("provider:with/symbols", "Model (test): v1/2"))
	if !ok || providerID != "provider:with/symbols" || modelID != "Model (test): v1/2" {
		t.Fatalf("encoded model contact ID did not parse: %q %q %v", providerID, modelID, ok)
	}
	providerID, modelID, ok = ParseModelContactID(ModelContactID("beeper", "gpt-5"))
	if !ok || providerID != "beeper" || modelID != "gpt-5" {
		t.Fatalf("model contact ID did not parse: %q %q %v", providerID, modelID, ok)
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
		Providers: map[string]ProviderConfig{
			provider.ID: provider,
		},
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	var decoded UserLoginMetadata
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	decodedProvider, ok := decoded.Providers["custom"]
	if !ok || decodedProvider.BaseURL != "https://example.com/v1/responses" {
		t.Fatalf("provider metadata did not round trip: %#v", decoded.Providers)
	}
	if decodedProvider.DefaultModel != "gpt-5" {
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
