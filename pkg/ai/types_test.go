package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestKnownProviderConstantsMatchTypeScriptSurface(t *testing.T) {
	providers := []Provider{
		ProviderMinimaxCN,
		ProviderMoonshotAI,
		ProviderMoonshotAICN,
		ProviderHuggingFace,
		ProviderFireworks,
		ProviderTogether,
		ProviderOpencode,
		ProviderOpencodeGo,
		ProviderKimiCoding,
		ProviderCloudflareWorkersAI,
		ProviderCloudflareAIGateway,
		ProviderXiaomi,
		ProviderXiaomiTokenPlanCN,
		ProviderXiaomiTokenPlanAMS,
		ProviderXiaomiTokenPlanSGP,
	}
	want := []string{
		"minimax-cn",
		"moonshotai",
		"moonshotai-cn",
		"huggingface",
		"fireworks",
		"together",
		"opencode",
		"opencode-go",
		"kimi-coding",
		"cloudflare-workers-ai",
		"cloudflare-ai-gateway",
		"xiaomi",
		"xiaomi-token-plan-cn",
		"xiaomi-token-plan-ams",
		"xiaomi-token-plan-sgp",
	}
	for index, provider := range providers {
		if string(provider) != want[index] {
			t.Fatalf("provider constant mismatch at %d: got %q want %q", index, provider, want[index])
		}
	}
}

func TestOpenAICompatTypesMarshalWithTypeScriptFieldNames(t *testing.T) {
	truth := true
	compat := OpenAICompletionsCompat{
		SupportsStore:              &truth,
		MaxTokensField:             "max_tokens",
		ThinkingFormat:             "openrouter",
		SupportsLongCacheRetention: &truth,
		OpenRouterRouting:          &OpenRouterRouting{Only: []string{"openai"}, MaxPrice: &OpenRouterMaxPrice{Prompt: 1.25}},
		VercelGatewayRouting:       &VercelGatewayRouting{Order: []string{"anthropic", "openai"}},
	}
	raw, err := json.Marshal(compat)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(raw)
	for _, want := range []string{"supportsStore", "maxTokensField", "thinkingFormat", "supportsLongCacheRetention", "openRouterRouting", "vercelGatewayRouting", "max_price"} {
		if !strings.Contains(encoded, want) {
			t.Fatalf("expected %q in encoded compat JSON: %s", want, encoded)
		}
	}
}
