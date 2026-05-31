package connector

import (
	"context"
	"embed"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

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
	if logoURL := compatString(model.Compat["provider_logo_url"]); logoURL != "" {
		if resolvedURL, err := aiServicesProviderLogoURL(provider.BaseURL, logoURL); err == nil {
			return remoteModelAvatar(logoURL, resolvedURL)
		}
	}
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
	if key := modelAvatarProviderKeyFromHints(model); key != "" {
		return key
	}
	if key := modelAvatarProviderKeyFromModelID(model.ID); key != "" {
		return key
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

func remoteModelAvatar(logoID string, logoURL string) *bridgev2.Avatar {
	return &bridgev2.Avatar{
		ID: networkid.AvatarID("ai-model-provider-url:" + logoID),
		Get: func(ctx context.Context) ([]byte, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, logoURL, nil)
			if err != nil {
				return nil, err
			}
			resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return nil, fmt.Errorf("provider logo %s returned HTTP %d", logoURL, resp.StatusCode)
			}
			const maxProviderLogoBytes = 2 << 20
			data, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderLogoBytes+1))
			if err != nil {
				return nil, err
			}
			if len(data) > maxProviderLogoBytes {
				return nil, fmt.Errorf("provider logo %s exceeded %d bytes", logoURL, maxProviderLogoBytes)
			}
			return data, nil
		},
	}
}

func aiServicesProviderLogoURL(proxyBaseURL string, providerLogoURL string) (string, error) {
	providerLogoURL = strings.TrimSpace(providerLogoURL)
	if providerLogoURL == "" {
		return "", fmt.Errorf("empty provider logo URL")
	}
	logo, err := url.Parse(providerLogoURL)
	if err != nil {
		return "", err
	}
	if logo.IsAbs() {
		if logo.Scheme != "http" && logo.Scheme != "https" {
			return "", fmt.Errorf("unsupported provider logo URL scheme %q", logo.Scheme)
		}
		logo.Fragment = ""
		return logo.String(), nil
	}
	if proxyBaseURL == "" {
		return "", fmt.Errorf("missing AI Services base URL for provider logo %q", providerLogoURL)
	}
	base, err := url.Parse(strings.TrimRight(normalizeResponsesBaseURL(proxyBaseURL), "/"))
	if err != nil {
		return "", err
	}
	if base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("invalid AI Services base URL %q", proxyBaseURL)
	}
	base.Path = joinURLPath(trimAIProxyProviderPath(base.Path), logo.Path)
	base.RawQuery = logo.RawQuery
	base.Fragment = ""
	return base.String(), nil
}

func joinURLPath(basePath string, relativePath string) string {
	basePath = strings.TrimRight(basePath, "/")
	relativePath = strings.TrimLeft(relativePath, "/")
	if relativePath == "" {
		if basePath == "" {
			return "/"
		}
		return basePath
	}
	return basePath + "/" + relativePath
}

func modelAvatarProviderKeyFromHints(model ai.Model) string {
	if model.Compat == nil {
		return ""
	}
	if key := providerLogoURLAvatarKey(compatString(model.Compat["provider_logo_url"])); key != "" {
		return key
	}
	if key := modelAvatarProviderKeyFromModelID(compatString(model.Compat["provider_model_id"])); key != "" {
		return key
	}
	return modelAvatarProviderKeyFromFamily(compatString(model.Compat["family"]))
}

func modelAvatarProviderKeyFromModelID(modelID string) string {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
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
	return ""
}

func modelAvatarProviderKeyFromFamily(family string) string {
	family = strings.ToLower(strings.TrimSpace(family))
	switch {
	case strings.Contains(family, "anthropic"), strings.Contains(family, "claude"):
		return "anthropic"
	case strings.Contains(family, "google"), strings.Contains(family, "gemini"), strings.Contains(family, "gemma"):
		return "google"
	case strings.Contains(family, "openai"), strings.Contains(family, "gpt-"), strings.HasPrefix(family, "gpt"), strings.HasPrefix(family, "o1"), strings.HasPrefix(family, "o3"), strings.HasPrefix(family, "o4"):
		return "openai"
	case strings.Contains(family, "deepseek"):
		return "deepseek"
	case strings.Contains(family, "xai"), strings.Contains(family, "x-ai"), strings.Contains(family, "grok"):
		return "xai"
	case strings.Contains(family, "zai"), strings.Contains(family, "z-ai"), strings.Contains(family, "glm"):
		return "zai"
	case strings.Contains(family, "minimax"):
		return "minimax"
	case strings.Contains(family, "moonshot"), strings.Contains(family, "kimi"):
		return "moonshotai"
	case strings.Contains(family, "xiaomi"), strings.Contains(family, "mimo"):
		return "xiaomi"
	case strings.Contains(family, "llama"):
		return "llama"
	}
	return ""
}

func providerLogoURLAvatarKey(providerLogoURL string) string {
	providerLogoURL = strings.ToLower(strings.TrimSpace(providerLogoURL))
	if providerLogoURL == "" {
		return ""
	}
	if beforeQuery, _, ok := strings.Cut(providerLogoURL, "?"); ok {
		providerLogoURL = beforeQuery
	}
	if lastSlash := strings.LastIndex(providerLogoURL, "/"); lastSlash >= 0 {
		providerLogoURL = providerLogoURL[lastSlash+1:]
	}
	providerLogoURL = strings.TrimSuffix(providerLogoURL, ".png")
	switch providerLogoURL {
	case "anthropic", "google", "openrouter", "openai", "deepseek", "minimax", "moonshotai", "xiaomi", "llama", "qwen", "inclusionai":
		return providerLogoURL
	case "x-ai", "xai":
		return "xai"
	case "z-ai", "zai":
		return "zai"
	case "meta-llama":
		return "llama"
	default:
		return ""
	}
}

func compatString(value any) string {
	if str, ok := value.(string); ok {
		return str
	}
	return ""
}
