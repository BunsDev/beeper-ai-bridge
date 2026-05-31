package connector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"

	agui "github.com/beeper/ai-bridge/pkg/ag-ui"
	"github.com/beeper/ai-bridge/pkg/agent/harness/session"
	aistream "github.com/beeper/ai-bridge/pkg/ai-stream"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"github.com/beeper/ai-bridge/pkg/msgconv"
)

func (cl *Client) sendCommandNotice(ctx context.Context, portal *bridgev2.Portal, text string) error {
	return cl.sendCommandNoticeWithAI(ctx, portal, text, false)
}

func (cl *Client) sendAICommandNotice(ctx context.Context, portal *bridgev2.Portal, text string) error {
	return cl.sendCommandNoticeWithAI(ctx, portal, text, true)
}

func (cl *Client) sendCommandNoticeWithAI(ctx context.Context, portal *bridgev2.Portal, text string, includeAI bool) error {
	if cl == nil || cl.Main == nil || cl.UserLogin == nil || portal == nil || portal.MXID == "" {
		return fmt.Errorf("portal room is not available to send command notice")
	}
	content := commandResponseContent(text)
	model := ""
	if roomConfig, _, err := cl.Main.ReadRoomConfig(ctx, portal.MXID); err == nil {
		if provider, modelID, err := cl.Main.ResolveProvider(ctx, cl.UserLogin, roomConfig); err == nil {
			cl.applyModelProfile(ctx, content, provider.ID, modelID)
			model = strings.Trim(provider.ID+"/"+modelID, "/")
		}
	}
	if content.BeeperPerMessageProfile == nil {
		content.BeeperPerMessageProfile = &event.BeeperPerMessageProfile{
			ID:          string(aiid.AssistantUserID()),
			Displayname: "AI",
			HasFallback: true,
		}
	}
	now := time.Now()
	messageID := networkid.MessageID("command-notice:" + session.CreateSessionID())
	extra := map[string]any(nil)
	if includeAI {
		agentID := content.BeeperPerMessageProfile.ID
		agentName := content.BeeperPerMessageProfile.Displayname
		if agentName == "" {
			agentName = "AI"
		}
		extra = map[string]any{
			aistream.BeeperAIKey: commandFinalAI(text, string(messageID), string(portal.MXID), model, agentID, agentName, now),
		}
	}
	cl.UserLogin.QueueRemoteEvent(&simplevent.PreConvertedMessage{
		EventMeta: simplevent.EventMeta{
			Type:      bridgev2.RemoteEventMessage,
			PortalKey: portal.PortalKey,
			Sender: bridgev2.EventSender{
				Sender: aiid.AssistantUserID(),
			},
			Timestamp: now,
		},
		ID: messageID,
		Data: &bridgev2.ConvertedMessage{Parts: []*bridgev2.ConvertedMessagePart{{
			ID:      aiid.PartID("command"),
			Type:    event.EventMessage,
			Content: content,
			Extra:   extra,
			DBMetadata: &aiid.MessageMetadata{
				Role:         "command",
				StreamStatus: "notice",
			},
		}}},
	})
	return nil
}

func commandFinalAI(text string, messageID string, threadID string, model string, agentID string, agentName string, now time.Time) aistream.BeeperAI {
	runID := "command-" + session.CreateSessionID()
	if agentID == "" {
		agentID = string(aiid.AssistantUserID())
	}
	if agentName == "" {
		agentName = "AI"
	}
	run := aistream.Run{
		ThreadID:   strings.TrimSpace(threadID),
		RunID:      runID,
		MessageID:  messageID,
		Model:      strings.TrimSpace(model),
		AgentID:    agentID,
		AgentName:  agentName,
		Data:       map[string]any{},
		Status:     aistream.Status{State: "complete", FinishReason: agui.FinishReasonStop},
		Final:      aistream.FinalDelivery{Delivery: "inline", PartsComplete: true},
		Usage:      agui.Usage{},
		Preview:    aistream.Preview{Text: text},
		ToolCallID: "",
	}
	if run.ThreadID == "" {
		run.ThreadID = runID
	}
	message := aistream.UIMessage{
		ID:   messageID,
		Role: agui.RoleAssistant,
		Parts: []aistream.MessagePart{{
			"type":    "text",
			"content": text,
			"state":   agui.PartStateDone,
		}},
	}
	return run.AIWithMessage(aistream.AIKindFinal, message)
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
