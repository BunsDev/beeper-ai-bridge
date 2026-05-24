package connector

import (
	"context"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"
)

var roomCaps = &event.RoomFeatures{
	ID: "com.beeper.ai.capabilities.v1",
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
	MaxTextLength:       20000,
	Reply:               event.CapLevelFullySupported,
	Edit:                event.CapLevelRejected,
	Delete:              event.CapLevelPartialSupport,
	Reaction:            event.CapLevelUnsupported,
	ReadReceipts:        false,
	TypingNotifications: false,
}

func (c *Connector) GetCapabilities() *bridgev2.NetworkGeneralCapabilities {
	return &bridgev2.NetworkGeneralCapabilities{
		Provisioning: bridgev2.ProvisioningCapabilities{
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
