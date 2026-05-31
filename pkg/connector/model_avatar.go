package connector

import (
	"context"
	"embed"
	"strings"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/id"
)

const defaultAIAssistantAvatarMXC = "mxc://beeper.com/51a668657dd9e0132cc823ad9402c6c2d0fc3321"

//go:embed modelassets/png/*.png
var modelAvatarAssets embed.FS

func defaultAIAssistantAvatar() *bridgev2.Avatar {
	return &bridgev2.Avatar{
		ID:  networkid.AvatarID(defaultAIAssistantAvatarMXC),
		MXC: id.ContentURIString(defaultAIAssistantAvatarMXC),
	}
}

func modelAvatar(provider aiid.ProviderConfig, model ai.Model) *bridgev2.Avatar {
	key := modelAvatarProviderKey(provider, model)
	assetPath := "modelassets/png/" + key + ".png"
	if _, err := modelAvatarAssets.ReadFile(assetPath); err != nil {
		return nil
	}
	return &bridgev2.Avatar{
		ID: networkid.AvatarID("ai-model-provider:" + key),
		Get: func(_ context.Context) ([]byte, error) {
			return modelAvatarAssets.ReadFile(assetPath)
		},
	}
}

func modelAvatarProviderKey(provider aiid.ProviderConfig, model ai.Model) string {
	modelID := strings.ToLower(model.ID)
	switch {
	case strings.HasPrefix(modelID, "claude-"), strings.HasPrefix(modelID, "anthropic/"):
		return "anthropic"
	case strings.HasPrefix(modelID, "gemini-"), strings.HasPrefix(modelID, "google/"):
		return "google"
	case strings.HasPrefix(modelID, "openrouter/"):
		return "openrouter"
	case strings.HasPrefix(modelID, "openai/"), strings.HasPrefix(modelID, "gpt-"), strings.HasPrefix(modelID, "o1"), strings.HasPrefix(modelID, "o3"), strings.HasPrefix(modelID, "o4"):
		return "openai"
	case strings.HasPrefix(modelID, "deepseek/"), strings.HasPrefix(modelID, "deepseek-"):
		return "deepseek"
	case strings.HasPrefix(modelID, "x-ai/"), strings.HasPrefix(modelID, "xai/"), strings.HasPrefix(modelID, "grok-"):
		return "xai"
	case strings.HasPrefix(modelID, "z-ai/"), strings.HasPrefix(modelID, "zai/"), strings.HasPrefix(modelID, "glm-"):
		return "zai"
	case strings.HasPrefix(modelID, "minimax/"):
		return "minimax"
	case strings.HasPrefix(modelID, "moonshotai/"), strings.HasPrefix(modelID, "moonshot/"), strings.HasPrefix(modelID, "kimi-"):
		return "moonshotai"
	case strings.HasPrefix(modelID, "xiaomi/"):
		return "xiaomi"
	case strings.Contains(modelID, "llama"):
		return "llama"
	}
	switch model.Provider {
	case ai.ProviderAnthropic:
		return "anthropic"
	case ai.ProviderGoogle, ai.ProviderGoogleVertex:
		return "google"
	case ai.ProviderOpenRouter:
		return "openrouter"
	case ai.ProviderOpenAI, ai.ProviderOpenAICodex:
		return "openai"
	case ai.ProviderDeepSeek:
		return "deepseek"
	case ai.ProviderXAI:
		return "xai"
	case ai.ProviderZai:
		return "zai"
	case ai.ProviderMinimax, ai.ProviderMinimaxCN:
		return "minimax"
	case ai.ProviderMoonshotAI, ai.ProviderMoonshotAICN, ai.ProviderKimiCoding:
		return "moonshotai"
	case ai.ProviderXiaomi, ai.ProviderXiaomiTokenPlanCN, ai.ProviderXiaomiTokenPlanAMS, ai.ProviderXiaomiTokenPlanSGP:
		return "xiaomi"
	}
	providerID := strings.ToLower(provider.ID)
	switch {
	case strings.Contains(providerID, "anthropic"):
		return "anthropic"
	case strings.Contains(providerID, "vertex"), strings.Contains(providerID, "google"):
		return "google"
	case strings.Contains(providerID, "openrouter"):
		return "openrouter"
	case strings.Contains(providerID, "openai"):
		return "openai"
	case strings.Contains(providerID, "deepseek"):
		return "deepseek"
	case strings.Contains(providerID, "xai"):
		return "xai"
	case strings.Contains(providerID, "zai"):
		return "zai"
	case strings.Contains(providerID, "minimax"):
		return "minimax"
	case strings.Contains(providerID, "moonshot"), strings.Contains(providerID, "kimi"):
		return "moonshotai"
	case strings.Contains(providerID, "xiaomi"):
		return "xiaomi"
	case strings.Contains(providerID, "llama"):
		return "llama"
	default:
		return "beeper-ai"
	}
}
