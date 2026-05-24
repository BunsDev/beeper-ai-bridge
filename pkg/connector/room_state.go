package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

type RoomConfig struct {
	LoginID       string `json:"login_id"`
	ProviderID    string `json:"provider_id"`
	ModelID       string `json:"model_id"`
	SystemPrompt  string `json:"system_prompt"`
	ThinkingLevel string `json:"thinking_level"`
	ToolsEnabled  bool   `json:"tools_enabled"`
	Cwd           string `json:"cwd"`
}

func (c *Connector) ReadRoomConfig(ctx context.Context, roomID id.RoomID, portalMeta *aiid.PortalMetadata) (RoomConfig, string, error) {
	config := RoomConfig{}
	if portalMeta != nil {
		config.LoginID = portalMeta.SelectedLoginID
		config.ProviderID = portalMeta.SelectedProviderID
		config.ModelID = portalMeta.SelectedModelID
		config.SystemPrompt = portalMeta.SystemPrompt
		config.ThinkingLevel = portalMeta.ThinkingLevel
		config.ToolsEnabled = portalMeta.ToolsEnabled
		config.Cwd = portalMeta.Cwd
	}
	reader, ok := c.Bridge.Matrix.(bridgev2.MatrixConnectorWithArbitraryRoomState)
	if !ok {
		return config, "", nil
	}
	stateType := event.Type{Type: c.Config.RoomStateEventType, Class: event.StateEventType}
	evt, err := reader.GetStateEvent(ctx, roomID, stateType, "")
	if errors.Is(err, mautrix.MNotFound) {
		return config, "", nil
	} else if err != nil {
		return RoomConfig{}, "", fmt.Errorf("failed to read %s state: %w", c.Config.RoomStateEventType, err)
	}
	if evt == nil {
		return config, "", nil
	}
	raw, err := json.Marshal(evt.Content.Raw)
	if err != nil {
		return RoomConfig{}, "", err
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return RoomConfig{}, "", err
	}
	return config, string(evt.ID), nil
}

func (c *Connector) ResolveProvider(ctx context.Context, login *bridgev2.UserLogin, roomConfig RoomConfig) (aiid.ProviderConfig, string, error) {
	meta := login.Metadata.(*aiid.UserLoginMetadata)
	ensureMetadataDefaults(meta, c.defaultProviderConfig())
	providerID := roomConfig.ProviderID
	if providerID == "" {
		providerID = meta.DefaultProviderID
	}
	provider, ok := meta.Providers[providerID]
	if !ok || !provider.Enabled {
		return aiid.ProviderConfig{}, "", fmt.Errorf("provider %s is not available for login %s", providerID, login.ID)
	}
	modelID := roomConfig.ModelID
	if modelID == "" {
		modelID = provider.DefaultModel
	}
	if modelID == "" {
		modelID = meta.DefaultModelID
	}
	if modelID == "" {
		return aiid.ProviderConfig{}, "", fmt.Errorf("provider %s has no selected model", providerID)
	}
	if len(provider.Models) > 0 && !providerHasModel(provider, modelID) {
		if _, ok := ai.GetModel(provider.Provider, modelID); !ok {
			return aiid.ProviderConfig{}, "", fmt.Errorf("model %s is not available for provider %s", modelID, providerID)
		}
	}
	return provider, modelID, nil
}

func loginIDFromConfig(config RoomConfig) networkid.UserLoginID {
	return networkid.UserLoginID(config.LoginID)
}

func providerHasModel(provider aiid.ProviderConfig, modelID string) bool {
	for _, model := range provider.Models {
		if model.ID == modelID {
			return true
		}
	}
	return false
}
