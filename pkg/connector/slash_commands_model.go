package connector

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"maunium.net/go/mautrix/bridgev2"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
)

func runModelCommand(cl *Client, ctx context.Context, portal *bridgev2.Portal, roomConfig RoomConfig, arg string, responder aiCommandResponder) error {
	return cl.applyModelCommand(ctx, portal, roomConfig, arg, responder)
}

func runReasoningCommand(cl *Client, ctx context.Context, portal *bridgev2.Portal, roomConfig RoomConfig, arg string, responder aiCommandResponder) error {
	return cl.applyReasoningCommand(ctx, portal, roomConfig, arg, responder)
}

func (cl *Client) applyModelCommand(ctx context.Context, portal *bridgev2.Portal, current RoomConfig, requested string, responder aiCommandResponder) error {
	if strings.TrimSpace(requested) == "" {
		provider, model, canonical, err := cl.resolveCanonicalRoomModel(ctx, current)
		if err != nil {
			return fmt.Errorf("AI room settings rejected: %v", err)
		}
		return responder.Reply(ctx, cl.modelStatusText(canonical, cl.reasoningLevelForModel(model, current), provider))
	}
	target := current
	providerID, modelID := splitModelRef(requested)
	if modelID == "" {
		return fmt.Errorf("AI room settings rejected: model is required")
	}
	target.ProviderID = providerID
	target.ModelID = modelID
	provider, model, canonical, err := cl.resolveCanonicalRoomModel(ctx, target)
	if err != nil {
		return fmt.Errorf("AI room settings rejected: %v", err)
	}
	if err = cl.validateReasoningLevel(model, target); err != nil {
		return fmt.Errorf("AI room settings rejected: %v", err)
	}
	target.ThinkingLevel = cl.reasoningLevelForModel(model, target)
	if _, err = cl.writeRoomModelState(ctx, portal, provider, model, canonical, target.ThinkingLevel); err != nil {
		return err
	}
	cl.refreshRoomCapabilities(ctx, portal)
	return responder.Reply(ctx, fmt.Sprintf("Model set to `%s`. Current reasoning is `%s`.", canonical, target.ThinkingLevel))
}

func (cl *Client) applyReasoningCommand(ctx context.Context, portal *bridgev2.Portal, current RoomConfig, requested string, responder aiCommandResponder) error {
	if strings.TrimSpace(requested) == "" {
		_, model, canonical, err := cl.resolveCanonicalRoomModel(ctx, current)
		if err != nil {
			return fmt.Errorf("AI room settings rejected: %v", err)
		}
		return responder.Reply(ctx, reasoningStatusText(cl.reasoningLevelForModel(model, current), canonical, model))
	}
	reasoning := strings.ToLower(strings.TrimSpace(requested))
	if !validRoomReasoningLevel(reasoning) {
		return fmt.Errorf("AI room settings rejected: reasoning level %q is invalid", requested)
	}
	target := current
	target.ThinkingLevel = reasoning
	provider, model, canonical, err := cl.resolveCanonicalRoomModel(ctx, target)
	if err != nil {
		return fmt.Errorf("AI room settings rejected: %v", err)
	}
	if err = cl.validateReasoningLevel(model, target); err != nil {
		return fmt.Errorf("AI room settings rejected: %v", err)
	}
	if _, err = cl.writeRoomModelState(ctx, portal, provider, model, canonical, reasoning); err != nil {
		return err
	}
	cl.refreshRoomCapabilities(ctx, portal)
	return responder.Reply(ctx, fmt.Sprintf("Reasoning set to `%s` for `%s`.", reasoning, canonical))
}

func displayReasoningLevel(level string) string {
	if level == "" {
		return string(ai.ModelThinkingLevelOff)
	}
	return level
}

func reasoningStatusText(current string, canonicalModel string, model ai.Model) string {
	return fmt.Sprintf("Current reasoning is `%s` for `%s`. Options: %s.", displayReasoningLevel(current), canonicalModel, reasoningOptionsText(model))
}

func reasoningOptionsText(model ai.Model) string {
	levels := ai.GetSupportedThinkingLevels(model)
	if len(levels) == 0 {
		levels = []ai.ModelThinkingLevel{ai.ModelThinkingLevelOff}
	}
	options := make([]string, 0, len(levels))
	for _, level := range levels {
		options = append(options, fmt.Sprintf("`%s`", level))
	}
	return strings.Join(options, ", ")
}

func (cl *Client) modelStatusText(currentModel string, currentReasoning string, currentProvider aiid.ProviderConfig) string {
	return fmt.Sprintf("Current model is `%s`. Current reasoning is `%s`. Options: %s.", currentModel, currentReasoning, cl.modelOptionsText(currentProvider))
}

func (cl *Client) modelOptionsText(currentProvider aiid.ProviderConfig) string {
	providers := map[string]aiid.ProviderConfig{}
	if cl != nil && cl.Main != nil && cl.UserLogin != nil {
		for id, provider := range cl.Main.providersForLogin(cl.UserLogin) {
			providers[id] = provider
		}
	}
	if currentProvider.ID != "" {
		providers[currentProvider.ID] = currentProvider
	}
	if len(providers) == 0 {
		return "`<provider>/<model>`"
	}
	providerIDs := slices.SortedFunc(maps.Keys(providers), compareProviderID)
	options := []string{}
	for _, providerID := range providerIDs {
		provider := providers[providerID]
		if len(provider.Models) == 0 && providerAllowsArbitraryModels(provider) {
			options = append(options, fmt.Sprintf("`%s/<model>`", provider.ID))
			continue
		}
		for _, model := range contactModels(provider) {
			options = append(options, fmt.Sprintf("`%s/%s`", provider.ID, model.ID))
		}
	}
	if len(options) == 0 {
		return "`<provider>/<model>`"
	}
	return strings.Join(options, ", ")
}

func validRoomReasoningLevel(level string) bool {
	switch level {
	case "", string(ai.ModelThinkingLevelOff), string(ai.ModelThinkingLevelMinimal), string(ai.ModelThinkingLevelLow), string(ai.ModelThinkingLevelMedium), string(ai.ModelThinkingLevelHigh), string(ai.ModelThinkingLevelXHigh):
		return true
	default:
		return false
	}
}
