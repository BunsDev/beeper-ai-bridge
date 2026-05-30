package connector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/beeper/ai-bridge/pkg/agent/harness/session"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"github.com/beeper/ai-bridge/pkg/msgconv"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
)

func (cl *Client) sendCommandNotice(ctx context.Context, portal *bridgev2.Portal, text string) error {
	if cl == nil || cl.UserLogin == nil || portal == nil || portal.MXID == "" {
		return fmt.Errorf("portal room is not available to send command notice")
	}
	content := commandResponseContent(text)
	if roomConfig, _, err := cl.Main.ReadRoomConfig(ctx, portal.MXID); err == nil {
		if provider, modelID, err := cl.Main.ResolveProvider(ctx, cl.UserLogin, roomConfig); err == nil {
			cl.applyModelProfile(ctx, content, provider.ID, modelID)
		}
	}
	if content.BeeperPerMessageProfile == nil {
		content.BeeperPerMessageProfile = &event.BeeperPerMessageProfile{
			ID:          string(aiid.AssistantUserID()),
			Displayname: "AI",
			HasFallback: true,
		}
	}
	cl.UserLogin.QueueRemoteEvent(&simplevent.PreConvertedMessage{
		EventMeta: simplevent.EventMeta{
			Type:      bridgev2.RemoteEventMessage,
			PortalKey: portal.PortalKey,
			Sender: bridgev2.EventSender{
				Sender: aiid.AssistantUserID(),
			},
			Timestamp: time.Now(),
		},
		ID: networkid.MessageID("command-notice:" + session.CreateSessionID()),
		Data: &bridgev2.ConvertedMessage{Parts: []*bridgev2.ConvertedMessagePart{{
			ID:      aiid.PartID("command"),
			Type:    event.EventMessage,
			Content: content,
			DBMetadata: &aiid.MessageMetadata{
				Role:         "command",
				StreamStatus: "notice",
			},
		}}},
	})
	return nil
}

func commandResponseContent(text string) *event.MessageEventContent {
	if strings.TrimSpace(text) == "" {
		return msgconv.TextContent(text)
	}
	content := format.RenderMarkdown(text, true, false)
	content.EnsureHasHTML()
	return &content
}

func commandRejectedError(text string) error {
	return bridgev2.WrapErrorInStatus(errors.New(text)).
		WithStatus(event.MessageStatusFail).
		WithErrorReason(event.MessageStatusUnsupported).
		WithErrorAsMessage().
		WithIsCertain(true).
		WithSendNotice(true)
}

func (cl *Client) commandHandledResponse(msg *bridgev2.MatrixMessage, status string) *bridgev2.MatrixMessageResponse {
	return &bridgev2.MatrixMessageResponse{DB: &database.Message{
		ID:        networkid.MessageID("command:" + session.CreateSessionID()),
		PartID:    aiid.PartID("command"),
		Room:      msg.Portal.PortalKey,
		SenderID:  cl.GetUserID(),
		Timestamp: matrixEventTime(msg.Event),
		Metadata: &aiid.MessageMetadata{
			Role:         "command",
			StreamStatus: status,
		},
	}}
}
