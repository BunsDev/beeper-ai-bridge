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

func TestModelSupportsAgentToolsDefaultsToTrue(t *testing.T) {
	if !modelSupportsAgentTools(ai.Model{}) {
		t.Fatal("models without catalog tool metadata should keep default tool behavior")
	}
}
