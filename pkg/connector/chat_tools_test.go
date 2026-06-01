package connector

import (
	"testing"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
)

func TestChatToolsSkipModelsWithoutToolSupport(t *testing.T) {
	client := &Client{}
	tools := client.chatTools(nil, &aiid.PortalMetadata{}, RoomConfig{}, aiid.ProviderConfig{}, ai.Model{
		Compat: map[string]any{"tools_supported": false},
	}, "")
	if len(tools) != 0 {
		t.Fatalf("expected no tools, got %#v", tools)
	}
}

func TestChatToolsSkipGoogleVertexImageModels(t *testing.T) {
	client := &Client{}
	tools := client.chatTools(nil, &aiid.PortalMetadata{}, RoomConfig{}, aiid.ProviderConfig{}, ai.Model{
		Provider: ai.ProviderGoogleVertex,
		Output:   []string{"image", "text"},
	}, "")
	if len(tools) != 0 {
		t.Fatalf("expected no tools for Google Vertex image model, got %#v", tools)
	}
}

func TestModelSupportsAgentToolsRequiresExplicitCatalogSupport(t *testing.T) {
	if modelSupportsAgentTools(ai.Model{}) {
		t.Fatal("models without catalog tool metadata should not expose agent tools")
	}
	if !modelSupportsAgentTools(ai.Model{Compat: map[string]any{"tools_supported": true}}) {
		t.Fatal("models with explicit catalog tool support should expose agent tools")
	}
}

func TestAIServicesToolURL(t *testing.T) {
	got, err := aiServicesToolURL("https://ai-services.example/dev", "web_search")
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://ai-services.example/dev/tools/web_search"; got != want {
		t.Fatalf("unexpected tool URL %q, want %q", got, want)
	}
}
