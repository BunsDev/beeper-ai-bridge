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
	portalMeta := portalMetadata(msg.Portal)
	roomConfig, _, err := cl.Main.ReadRoomConfig(ctx, msg.Portal.MXID, portalMeta)
	if err != nil {
		return nil, true, err
	}
	switch cmd.name {
	case "model":
		if cmd.arg == "" {
			cl.queueCommandNotice(msg.Portal, "Usage: /model <model>")
			return cl.commandHandledResponse(msg, "usage"), true, nil
		}
		if err = cl.applyModelCommand(ctx, msg.Portal, portalMeta, roomConfig, cmd.arg); err != nil {
			cl.queueCommandNotice(msg.Portal, err.Error())
			return cl.commandHandledResponse(msg, "rejected"), true, nil
		}
	case "reasoning", "reasoniing":
		if cmd.arg == "" {
			cl.queueCommandNotice(msg.Portal, "Usage: /reasoning <off|low|medium|high>")
			return cl.commandHandledResponse(msg, "usage"), true, nil
		}
		if err = cl.applyReasoningCommand(ctx, msg.Portal, portalMeta, roomConfig, cmd.arg); err != nil {
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
		eventID, err := cl.writeRoomPromptState(ctx, msg.Portal, prompt)
		if err != nil {
			return nil, true, err
		}
		portalMeta.AdditionalPrompt = prompt
		portalMeta.RoomPromptEventID = string(eventID)
		if err = msg.Portal.Save(ctx); err != nil {
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

func (cl *Client) applyModelCommand(ctx context.Context, portal *bridgev2.Portal, meta *aiid.PortalMetadata, current RoomConfig, requested string) error {
	target := current
	providerID, modelID := splitModelRef(requested)
	if modelID == "" {
		return fmt.Errorf("AI room settings rejected: model is required. Restored %s.", cl.restoreModelLabel(meta))
	}
	target.ProviderID = providerID
	target.ModelID = modelID
	provider, model, canonical, err := cl.resolveCanonicalRoomModel(ctx, target)
	if err != nil {
		if restoreErr := cl.restoreRoomModelState(ctx, portal, meta); restoreErr != nil {
			return fmt.Errorf("AI room settings rejected: %v. Failed to restore previous settings: %v.", err, restoreErr)
		}
		return fmt.Errorf("AI room settings rejected: %v. Restored %s.", err, cl.restoreModelLabel(meta))
	}
	if err = cl.validateReasoningLevel(model, target); err != nil {
		if restoreErr := cl.restoreRoomModelState(ctx, portal, meta); restoreErr != nil {
			return fmt.Errorf("AI room settings rejected: %v. Failed to restore previous settings: %v.", err, restoreErr)
		}
		return fmt.Errorf("AI room settings rejected: %v. Restored %s.", err, cl.restoreModelLabel(meta))
	}
	eventID, err := cl.writeRoomModelState(ctx, portal, canonical, target.ThinkingLevel)
	if err != nil {
		return err
	}
	meta.SelectedProviderID = provider.ID
	meta.SelectedModelID = model.ID
	meta.ThinkingLevel = target.ThinkingLevel
	meta.RoomStateEventID = string(eventID)
	if err = portal.Save(ctx); err != nil {
		return err
	}
	cl.queueCommandNotice(portal, fmt.Sprintf("Model set to `%s`.", canonical))
	return nil
}

func (cl *Client) applyReasoningCommand(ctx context.Context, portal *bridgev2.Portal, meta *aiid.PortalMetadata, current RoomConfig, requested string) error {
	reasoning := strings.ToLower(strings.TrimSpace(requested))
	if !validRoomReasoningLevel(reasoning) {
		if restoreErr := cl.restoreRoomModelState(ctx, portal, meta); restoreErr != nil {
			return fmt.Errorf("AI room settings rejected: reasoning level %q is invalid. Failed to restore previous settings: %v.", requested, restoreErr)
		}
		return fmt.Errorf("AI room settings rejected: reasoning level %q is invalid. Restored %s.", requested, cl.restoreModelLabel(meta))
	}
	target := current
	target.ThinkingLevel = reasoning
	provider, model, canonical, err := cl.resolveCanonicalRoomModel(ctx, target)
	if err != nil {
		if restoreErr := cl.restoreRoomModelState(ctx, portal, meta); restoreErr != nil {
			return fmt.Errorf("AI room settings rejected: %v. Failed to restore previous settings: %v.", err, restoreErr)
		}
		return fmt.Errorf("AI room settings rejected: %v. Restored %s.", err, cl.restoreModelLabel(meta))
	}
	if err = cl.validateReasoningLevel(model, target); err != nil {
		if restoreErr := cl.restoreRoomModelState(ctx, portal, meta); restoreErr != nil {
			return fmt.Errorf("AI room settings rejected: %v. Failed to restore previous settings: %v.", err, restoreErr)
		}
		return fmt.Errorf("AI room settings rejected: %v. Restored %s.", err, cl.restoreModelLabel(meta))
	}
	eventID, err := cl.writeRoomModelState(ctx, portal, canonical, reasoning)
	if err != nil {
		return err
	}
	meta.SelectedProviderID = provider.ID
	meta.SelectedModelID = model.ID
	meta.ThinkingLevel = reasoning
	meta.RoomStateEventID = string(eventID)
	if err = portal.Save(ctx); err != nil {
		return err
	}
	cl.queueCommandNotice(portal, fmt.Sprintf("Reasoning set to `%s` for `%s`.", reasoning, canonical))
	return nil
}

func (cl *Client) normalizeRoomStateForPrompt(ctx context.Context, msg *bridgev2.MatrixMessage, config RoomConfig, stateEventID string) (RoomConfig, *bridgev2.MatrixMessageResponse, bool, error) {
	if msg == nil || msg.Portal == nil {
		return config, nil, false, nil
	}
	meta := portalMetadata(msg.Portal)
	provider, model, canonical, err := cl.resolveCanonicalRoomModel(ctx, config)
	if err != nil {
		if !config.modelStatePresent {
			return config, nil, false, err
		}
		if restoreErr := cl.restoreRoomModelState(ctx, msg.Portal, meta); restoreErr != nil {
			return config, nil, false, restoreErr
		}
		cl.queueCommandNotice(msg.Portal, fmt.Sprintf("AI room settings rejected: %v. Restored %s.", err, cl.restoreModelLabel(meta)))
		return config, cl.commandHandledResponse(msg, "invalid-settings"), true, nil
	}
	if config.ThinkingLevel != "" && !validRoomReasoningLevel(config.ThinkingLevel) {
		if !config.modelStatePresent {
			return config, nil, false, fmt.Errorf("reasoning level %q is invalid", config.ThinkingLevel)
		}
		if restoreErr := cl.restoreRoomModelState(ctx, msg.Portal, meta); restoreErr != nil {
			return config, nil, false, restoreErr
		}
		cl.queueCommandNotice(msg.Portal, fmt.Sprintf("AI room settings rejected: reasoning level %q is invalid. Restored %s.", config.ThinkingLevel, cl.restoreModelLabel(meta)))
		return config, cl.commandHandledResponse(msg, "invalid-settings"), true, nil
	}
	if err = cl.validateReasoningLevel(model, config); err != nil {
		if !config.modelStatePresent {
			return config, nil, false, err
		}
		if restoreErr := cl.restoreRoomModelState(ctx, msg.Portal, meta); restoreErr != nil {
			return config, nil, false, restoreErr
		}
		cl.queueCommandNotice(msg.Portal, fmt.Sprintf("AI room settings rejected: %v. Restored %s.", err, cl.restoreModelLabel(meta)))
		return config, cl.commandHandledResponse(msg, "invalid-settings"), true, nil
	}
	modelEventID := config.modelStateEventID
	normalized := config.modelStatePresent && (config.modelStateModel != canonical || config.modelStateReason != config.ThinkingLevel)
	if normalized {
		var eventID string
		if eventID, err = cl.writeRoomModelState(ctx, msg.Portal, canonical, config.ThinkingLevel); err != nil {
			return config, nil, false, err
		}
		if eventID != "" {
			modelEventID = eventID
		}
		cl.queueCommandNotice(msg.Portal, fmt.Sprintf("AI room settings normalized to `%s`.", canonical))
	} else if roomStateChanged(meta, config, provider.ID, model.ID, modelEventID) {
		cl.queueCommandNotice(msg.Portal, fmt.Sprintf("AI room settings accepted: `%s`.", canonical))
	}
	meta.SelectedProviderID = provider.ID
	meta.SelectedModelID = model.ID
	meta.ThinkingLevel = config.ThinkingLevel
	meta.AdditionalPrompt = config.AdditionalPrompt
	meta.RoomStateEventID = modelEventID
	meta.RoomPromptEventID = config.promptStateEventID
	if err = msg.Portal.Save(ctx); err != nil {
		return config, nil, false, err
	}
	config.ProviderID = provider.ID
	config.ModelID = model.ID
	return config, nil, false, nil
}

func roomStateChanged(meta *aiid.PortalMetadata, config RoomConfig, providerID string, modelID string, stateEventID string) bool {
	if meta == nil {
		return true
	}
	if config.modelStatePresent && (meta.SelectedProviderID != providerID || meta.SelectedModelID != modelID || meta.ThinkingLevel != config.ThinkingLevel || meta.RoomStateEventID != stateEventID) {
		return true
	}
	if config.promptStateEventID != "" && (meta.AdditionalPrompt != config.AdditionalPrompt || meta.RoomPromptEventID != config.promptStateEventID) {
		return true
	}
	return false
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

func (cl *Client) restoreRoomModelState(ctx context.Context, portal *bridgev2.Portal, meta *aiid.PortalMetadata) error {
	restore := RoomConfig{}
	if meta != nil {
		restore.ProviderID = meta.SelectedProviderID
		restore.ModelID = meta.SelectedModelID
		restore.ThinkingLevel = meta.ThinkingLevel
	}
	_, _, canonical, err := cl.resolveCanonicalRoomModel(ctx, restore)
	if err != nil {
		return err
	}
	if _, err = cl.writeRoomModelState(ctx, portal, canonical, restore.ThinkingLevel); err != nil {
		return err
	}
	return nil
}

func (cl *Client) restoreModelLabel(meta *aiid.PortalMetadata) string {
	if meta != nil && meta.SelectedProviderID != "" && meta.SelectedModelID != "" {
		return "`" + meta.SelectedProviderID + "/" + meta.SelectedModelID + "`"
	}
	metaProvider := ""
	if meta != nil {
		metaProvider = meta.SelectedProviderID
	}
	config := RoomConfig{ProviderID: metaProvider}
	provider, modelID, err := cl.Main.ResolveProvider(context.Background(), cl.UserLogin, config)
	if err != nil {
		return "previous settings"
	}
	return "`" + provider.ID + "/" + modelID + "`"
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
	if cl == nil || cl.Main == nil || cl.Main.Bridge == nil || cl.Main.Bridge.Bot == nil {
		return "", fmt.Errorf("bridge bot is not available to write room state")
	}
	resp, err := cl.Main.Bridge.Bot.SendState(ctx, portal.MXID, event.Type{Type: stateType, Class: event.StateEventType}, "", &event.Content{Raw: content}, time.Now())
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
				Sender: aiid.AssistantUserID("system", "settings"),
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
