package connector

import (
	"context"

	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"
)

var roomCaps = &event.RoomFeatures{
	Formatting: event.FormattingFeatureMap{
		event.FmtBold:          event.CapLevelPartialSupport,
		event.FmtItalic:        event.CapLevelPartialSupport,
		event.FmtStrikethrough: event.CapLevelPartialSupport,
		event.FmtInlineCode:    event.CapLevelPartialSupport,
		event.FmtCodeBlock:     event.CapLevelPartialSupport,
		event.FmtBlockquote:    event.CapLevelPartialSupport,
		event.FmtInlineLink:    event.CapLevelPartialSupport,
		event.FmtUnorderedList: event.CapLevelPartialSupport,
		event.FmtOrderedList:   event.CapLevelPartialSupport,
	},
	File: event.FileFeatureMap{
		event.MsgImage: {
			MimeTypes: map[string]event.CapabilitySupportLevel{
				"image/png":  event.CapLevelFullySupported,
				"image/jpeg": event.CapLevelFullySupported,
				"image/webp": event.CapLevelFullySupported,
				"image/gif":  event.CapLevelPartialSupport,
			},
			MaxSize:          20 * 1024 * 1024,
			Caption:          event.CapLevelFullySupported,
			MaxCaptionLength: 20000,
		},
	},
	State: event.StateFeatureMap{
		aiid.RoomToolsType:                      {Level: event.CapLevelFullySupported},
		aiid.RoomModelType:                      {Level: event.CapLevelFullySupported},
		aiid.RoomPromptType:                     {Level: event.CapLevelFullySupported},
		event.StateRoomName.Type:                {Level: event.CapLevelFullySupported},
		event.StateTopic.Type:                   {Level: event.CapLevelFullySupported},
		event.StateBeeperDisappearingTimer.Type: {Level: event.CapLevelFullySupported},
	},
	MaxTextLength: 20000,
	Reply:         event.CapLevelFullySupported,
	Edit:          event.CapLevelRejected,
	Delete:        event.CapLevelPartialSupport,
	DisappearingTimer: &event.DisappearingTimerCapability{
		Types: []event.DisappearingType{
			event.DisappearingTypeAfterSend,
			event.DisappearingTypeAfterRead,
		},
	},
	Reaction:            event.CapLevelUnsupported,
	ReadReceipts:        false,
	TypingNotifications: true,
}

func (c *Connector) GetCapabilities() *bridgev2.NetworkGeneralCapabilities {
	return &bridgev2.NetworkGeneralCapabilities{
		Provisioning: bridgev2.ProvisioningCapabilities{
			ResolveIdentifier: bridgev2.ResolveIdentifierCapabilities{
				CreateDM:       true,
				LookupUsername: true,
				ContactList:    true,
				Search:         true,
			},
			GroupCreation: map[string]bridgev2.GroupTypeCapabilities{
				"ai": {
					TypeDescription: "AI session",
					Name:            bridgev2.GroupFieldCapability{Allowed: true},
					Topic:           bridgev2.GroupFieldCapability{Allowed: true},
					Participants:    bridgev2.GroupFieldCapability{Allowed: false},
					Avatar:          bridgev2.GroupFieldCapability{Allowed: false},
					Username:        bridgev2.GroupFieldCapability{Allowed: false},
					Parent:          bridgev2.GroupFieldCapability{Allowed: false},
					Disappear:       bridgev2.GroupFieldCapability{Allowed: false},
				},
			},
		},
	}
}

func (cl *Client) GetCapabilities(ctx context.Context, portal *bridgev2.Portal) *event.RoomFeatures {
	return roomCaps
}
