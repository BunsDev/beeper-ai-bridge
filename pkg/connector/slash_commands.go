package connector

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/beeper/ai-bridge/pkg/agent/harness/session"
	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"github.com/beeper/ai-bridge/pkg/msgconv"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/event"
)

type aiSlashCommand struct {
	name string
	arg  string
}

func parseAISlashCommand(body string) (aiSlashCommand, bool) {
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, "/") {
		return aiSlashCommand{}, false
	}
	name, arg, _ := strings.Cut(strings.TrimPrefix(body, "/"), " ")
	name = strings.ToLower(strings.TrimSpace(name))
	arg = strings.TrimSpace(arg)
	switch name {
	case "model", "reasoning", "reasoniing", "system-prompt":
		return aiSlashCommand{name: name, arg: arg}, true
	default:
		return aiSlashCommand{}, false
	}
}

func (cl *Client) handleAISlashCommand(ctx context.Context, msg *bridgev2.MatrixMessage) (*bridgev2.MatrixMessageResponse, bool, error) {
	if msg == nil || msg.Content == nil {
		return nil, false, nil
	}
	cmd, ok := parseAISlashCommand(msg.Content.Body)
	if !ok {
		return nil, false, nil
	}
	if msg.Portal == nil {
		return nil, true, fmt.Errorf("missing portal for AI command")
	}
	roomConfig, _, err := cl.Main.ReadRoomConfig(ctx, msg.Portal.MXID)
	if err != nil {
		return nil, true, err
	}
	switch cmd.name {
	case "model":
		if cmd.arg == "" {
			cl.queueCommandNotice(msg.Portal, "Usage: /model <model>")
			return cl.commandHandledResponse(msg, "usage"), true, nil
		}
		if err = cl.applyModelCommand(ctx, msg.Portal, roomConfig, cmd.arg); err != nil {
			cl.queueCommandNotice(msg.Portal, err.Error())
			return cl.commandHandledResponse(msg, "rejected"), true, nil
		}
	case "reasoning", "reasoniing":
		if cmd.arg == "" {
			cl.queueCommandNotice(msg.Portal, "Usage: /reasoning <off|low|medium|high>")
			return cl.commandHandledResponse(msg, "usage"), true, nil
		}
		if err = cl.applyReasoningCommand(ctx, msg.Portal, roomConfig, cmd.arg); err != nil {
			cl.queueCommandNotice(msg.Portal, err.Error())
			return cl.commandHandledResponse(msg, "rejected"), true, nil
		}
	case "system-prompt":
		if cmd.arg == "" {
			cl.queueCommandNotice(msg.Portal, "Usage: /system-prompt <prompt|clear>")
			return cl.commandHandledResponse(msg, "usage"), true, nil
		}
		prompt := cmd.arg
		if strings.EqualFold(prompt, "clear") || strings.EqualFold(prompt, "reset") {
			prompt = ""
		}
		if _, err := cl.writeRoomPromptState(ctx, msg.Portal, prompt); err != nil {
			return nil, true, err
		}
		if prompt == "" {
			cl.queueCommandNotice(msg.Portal, "System prompt cleared.")
		} else {
			cl.queueCommandNotice(msg.Portal, "System prompt updated.")
		}
	}
	return cl.commandHandledResponse(msg, cmd.name), true, nil
}

func (cl *Client) applyModelCommand(ctx context.Context, portal *bridgev2.Portal, current RoomConfig, requested string) error {
	target := current
	providerID, modelID := splitModelRef(requested)
	if modelID == "" {
		return fmt.Errorf("AI room settings rejected: model is required")
	}
	target.ProviderID = providerID
	target.ModelID = modelID
	_, model, canonical, err := cl.resolveCanonicalRoomModel(ctx, target)
	if err != nil {
		return fmt.Errorf("AI room settings rejected: %v", err)
	}
	if err = cl.validateReasoningLevel(model, target); err != nil {
		return fmt.Errorf("AI room settings rejected: %v", err)
	}
	if _, err = cl.writeRoomModelState(ctx, portal, canonical, target.ThinkingLevel); err != nil {
		return err
	}
	cl.queueCommandNotice(portal, fmt.Sprintf("Model set to `%s`.", canonical))
	return nil
}

func (cl *Client) applyReasoningCommand(ctx context.Context, portal *bridgev2.Portal, current RoomConfig, requested string) error {
	reasoning := strings.ToLower(strings.TrimSpace(requested))
	if !validRoomReasoningLevel(reasoning) {
		return fmt.Errorf("AI room settings rejected: reasoning level %q is invalid", requested)
	}
	target := current
	target.ThinkingLevel = reasoning
	_, model, canonical, err := cl.resolveCanonicalRoomModel(ctx, target)
	if err != nil {
		return fmt.Errorf("AI room settings rejected: %v", err)
	}
	if err = cl.validateReasoningLevel(model, target); err != nil {
		return fmt.Errorf("AI room settings rejected: %v", err)
	}
	if _, err = cl.writeRoomModelState(ctx, portal, canonical, reasoning); err != nil {
		return err
	}
	cl.queueCommandNotice(portal, fmt.Sprintf("Reasoning set to `%s` for `%s`.", reasoning, canonical))
	return nil
}

func (cl *Client) normalizeRoomStateForPrompt(ctx context.Context, msg *bridgev2.MatrixMessage, config RoomConfig) (RoomConfig, *bridgev2.MatrixMessageResponse, bool, error) {
	if msg == nil || msg.Portal == nil {
		return config, nil, false, nil
	}
	provider, model, canonical, err := cl.resolveCanonicalRoomModel(ctx, config)
	if err != nil {
		if !config.modelStatePresent {
			return config, nil, false, err
		}
		cl.queueCommandNotice(msg.Portal, fmt.Sprintf("AI room settings rejected: %v.", err))
		return config, cl.commandHandledResponse(msg, "invalid-settings"), true, nil
	}
	if config.ThinkingLevel != "" && !validRoomReasoningLevel(config.ThinkingLevel) {
		if !config.modelStatePresent {
			return config, nil, false, fmt.Errorf("reasoning level %q is invalid", config.ThinkingLevel)
		}
		cl.queueCommandNotice(msg.Portal, fmt.Sprintf("AI room settings rejected: reasoning level %q is invalid.", config.ThinkingLevel))
		return config, cl.commandHandledResponse(msg, "invalid-settings"), true, nil
	}
	if err = cl.validateReasoningLevel(model, config); err != nil {
		if !config.modelStatePresent {
			return config, nil, false, err
		}
		cl.queueCommandNotice(msg.Portal, fmt.Sprintf("AI room settings rejected: %v.", err))
		return config, cl.commandHandledResponse(msg, "invalid-settings"), true, nil
	}
	normalized := config.modelStatePresent && (config.modelStateModel != canonical || config.modelStateReason != config.ThinkingLevel)
	if normalized {
		if _, err = cl.writeRoomModelState(ctx, msg.Portal, canonical, config.ThinkingLevel); err != nil {
			return config, nil, false, err
		}
		cl.queueCommandNotice(msg.Portal, fmt.Sprintf("AI room settings normalized to `%s`.", canonical))
	}
	config.ProviderID = provider.ID
	config.ModelID = model.ID
	return config, nil, false, nil
}

func (cl *Client) resolveCanonicalRoomModel(ctx context.Context, config RoomConfig) (aiid.ProviderConfig, ai.Model, string, error) {
	provider, modelID, err := cl.Main.ResolveProvider(ctx, cl.UserLogin, config)
	if err != nil {
		return aiid.ProviderConfig{}, ai.Model{}, "", err
	}
	model := cl.Main.ModelForProvider(provider, modelID)
	return provider, model, provider.ID + "/" + model.ID, nil
}

func validRoomReasoningLevel(level string) bool {
	switch level {
	case "", string(ai.ModelThinkingLevelOff), string(ai.ModelThinkingLevelLow), string(ai.ModelThinkingLevelMedium), string(ai.ModelThinkingLevelHigh):
		return true
	default:
		return false
	}
}

func (cl *Client) writeRoomModelState(ctx context.Context, portal *bridgev2.Portal, canonicalModel string, reasoning string) (string, error) {
	content := map[string]any{"model": canonicalModel}
	if reasoning != "" {
		content["reasoning"] = reasoning
	}
	return cl.writeAIRoomState(ctx, portal, aiid.RoomModelType, content)
}

func (cl *Client) writeRoomPromptState(ctx context.Context, portal *bridgev2.Portal, prompt string) (string, error) {
	return cl.writeAIRoomState(ctx, portal, aiid.RoomPromptType, map[string]any{"prompt": strings.TrimSpace(prompt)})
}

func (cl *Client) writeAIRoomState(ctx context.Context, portal *bridgev2.Portal, stateType string, content map[string]any) (string, error) {
	if portal == nil || portal.MXID == "" {
		return "", fmt.Errorf("portal room is not available to write room state")
	}
	resp, err := portal.Internal().SendStateWithIntentOrBot(ctx, nil, event.Type{Type: stateType, Class: event.StateEventType}, "", &event.Content{Raw: content}, time.Now())
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", nil
	}
	return string(resp.EventID), nil
}

func (cl *Client) queueCommandNotice(portal *bridgev2.Portal, text string) {
	if cl == nil || cl.UserLogin == nil || portal == nil {
		return
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
		ID: networkid.MessageID("settings:" + session.CreateSessionID()),
		Data: &bridgev2.ConvertedMessage{Parts: []*bridgev2.ConvertedMessagePart{{
			ID:      aiid.PartID("notice"),
			Type:    event.EventMessage,
			Content: msgconv.NoticeContent(text),
			DBMetadata: &aiid.MessageMetadata{
				Role:         "assistant",
				StreamStatus: "done",
			},
		}}},
	})
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
