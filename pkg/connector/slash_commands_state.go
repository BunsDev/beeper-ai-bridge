package connector

import (
	"context"
	"fmt"
	"time"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
)

func (cl *Client) normalizeRoomStateForPrompt(ctx context.Context, msg *bridgev2.MatrixMessage, config RoomConfig) (RoomConfig, *bridgev2.MatrixMessageResponse, bool, error) {
	if msg == nil || msg.Portal == nil {
		return config, nil, false, nil
	}
	provider, model, canonical, err := cl.resolveCanonicalRoomModel(ctx, config)
	if err != nil {
		if !config.modelStatePresent {
			return config, nil, false, err
		}
		cl.logAIRoomSettingsError(ctx, msg, err, "AI room settings rejected")
		if noticeErr := cl.sendCommandNotice(ctx, msg.Portal, fmt.Sprintf("AI room settings rejected: %v.", err)); noticeErr != nil {
			return config, nil, false, noticeErr
		}
		return config, cl.commandHandledResponse(msg, "invalid-settings"), true, nil
	}
	if config.ThinkingLevel != "" && !validRoomReasoningLevel(config.ThinkingLevel) {
		err = fmt.Errorf("reasoning level %q is invalid", config.ThinkingLevel)
		if !config.modelStatePresent {
			return config, nil, false, err
		}
		cl.logAIRoomSettingsError(ctx, msg, err, "AI room settings rejected")
		if noticeErr := cl.sendCommandNotice(ctx, msg.Portal, fmt.Sprintf("AI room settings rejected: reasoning level %q is invalid.", config.ThinkingLevel)); noticeErr != nil {
			return config, nil, false, noticeErr
		}
		return config, cl.commandHandledResponse(msg, "invalid-settings"), true, nil
	}
	if err = cl.validateReasoningLevel(model, config); err != nil {
		if !config.modelStatePresent {
			return config, nil, false, err
		}
		cl.logAIRoomSettingsError(ctx, msg, err, "AI room settings rejected")
		if noticeErr := cl.sendCommandNotice(ctx, msg.Portal, fmt.Sprintf("AI room settings rejected: %v.", err)); noticeErr != nil {
			return config, nil, false, noticeErr
		}
		return config, cl.commandHandledResponse(msg, "invalid-settings"), true, nil
	}
	config.ThinkingLevel = cl.reasoningLevelForModel(model, config)
	normalized := config.modelStatePresent && (config.modelStateModel != canonical || config.modelStateReason != config.ThinkingLevel)
	if normalized {
		if _, err = cl.writeRoomModelState(ctx, msg.Portal, provider, model, canonical, config.ThinkingLevel); err != nil {
			return config, nil, false, err
		}
		cl.refreshRoomCapabilities(ctx, msg.Portal)
		if noticeErr := cl.sendCommandNotice(ctx, msg.Portal, fmt.Sprintf("AI room settings normalized to `%s`.", canonical)); noticeErr != nil {
			return config, nil, false, noticeErr
		}
	}
	config.ProviderID = provider.ID
	config.ModelID = model.ID
	return config, nil, false, nil
}

func (cl *Client) logAIRoomSettingsError(ctx context.Context, msg *bridgev2.MatrixMessage, err error, message string) {
	logCtx := zerolog.Ctx(ctx).With().Str("action", "ai_room_settings")
	if cl != nil && cl.UserLogin != nil {
		logCtx = logCtx.Str("login_id", string(cl.UserLogin.ID))
	}
	log := logCtx.Logger()
	event := log.Error().Err(err)
	if msg != nil {
		if msg.Portal != nil {
			event = event.
				Str("portal_id", string(msg.Portal.ID)).
				Str("portal_receiver", string(msg.Portal.Receiver)).
				Str("portal_mxid", string(msg.Portal.MXID))
		}
		if msg.Event != nil {
			event = event.
				Str("event_id", string(msg.Event.ID)).
				Str("event_type", string(msg.Event.Type.Type)).
				Str("sender", string(msg.Event.Sender))
		}
	}
	event.Msg(message)
}

func (cl *Client) refreshRoomCapabilities(ctx context.Context, portal *bridgev2.Portal) {
	if cl == nil || cl.UserLogin == nil || portal == nil {
		return
	}
	portal.UpdateCapabilities(ctx, cl.UserLogin, true)
}

func (cl *Client) resolveCanonicalRoomModel(ctx context.Context, config RoomConfig) (aiid.ProviderConfig, ai.Model, string, error) {
	provider, modelID, err := cl.resolveProvider(ctx, config)
	if err != nil {
		return aiid.ProviderConfig{}, ai.Model{}, "", err
	}
	model := cl.Main.ModelForProvider(provider, modelID)
	return provider, model, provider.ID + "/" + model.ID, nil
}

func (cl *Client) writeRoomModelState(ctx context.Context, portal *bridgev2.Portal, provider aiid.ProviderConfig, model ai.Model, canonicalModel string, reasoning string) (string, error) {
	content := map[string]any{"model": canonicalModel}
	if reasoning != "" {
		content["reasoning"] = reasoning
	}
	eventID, err := cl.writeAIRoomState(ctx, portal, aiid.RoomModelType, content)
	if err != nil {
		return eventID, err
	}
	if cl == nil || cl.UserLogin == nil || portal == nil {
		return eventID, nil
	}
	topic := modelRoomDescription(provider, model)
	portal.UpdateInfo(ctx, &bridgev2.ChatInfo{
		Topic:  &topic,
		Avatar: defaultAIAssistantAvatar(),
	}, cl.UserLogin, nil, time.Now())
	return eventID, nil
}

func modelRoomDescription(provider aiid.ProviderConfig, model ai.Model) string {
	return "AI Chat with " + modelDisplayName(provider, model)
}

func modelWelcomeNoticeText(provider aiid.ProviderConfig, model ai.Model) string {
	return "You are chatting with " + modelDisplayName(provider, model) + ". AI can make mistakes."
}

func (cl *Client) writeAIRoomState(ctx context.Context, portal *bridgev2.Portal, stateType string, content map[string]any) (string, error) {
	return cl.Main.aiRoomStateStore().Write(ctx, portal, stateType, content)
}
