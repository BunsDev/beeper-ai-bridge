package ai

import (
	"os"
	"path/filepath"
)

var apiKeyEnvVars = map[string][]string{
	"github-copilot":         {"COPILOT_GITHUB_TOKEN"},
	"anthropic":              {"ANTHROPIC_OAUTH_TOKEN", "ANTHROPIC_API_KEY"},
	"openai":                 {"OPENAI_API_KEY"},
	"azure-openai-responses": {"AZURE_OPENAI_API_KEY"},
	"deepseek":               {"DEEPSEEK_API_KEY"},
	"google":                 {"GEMINI_API_KEY"},
	"google-vertex":          {"GOOGLE_CLOUD_API_KEY"},
	"groq":                   {"GROQ_API_KEY"},
	"cerebras":               {"CEREBRAS_API_KEY"},
	"xai":                    {"XAI_API_KEY"},
	"openrouter":             {"OPENROUTER_API_KEY"},
	"vercel-ai-gateway":      {"AI_GATEWAY_API_KEY"},
	"zai":                    {"ZAI_API_KEY"},
	"mistral":                {"MISTRAL_API_KEY"},
	"minimax":                {"MINIMAX_API_KEY"},
	"minimax-cn":             {"MINIMAX_CN_API_KEY"},
	"moonshotai":             {"MOONSHOT_API_KEY"},
	"moonshotai-cn":          {"MOONSHOT_API_KEY"},
	"huggingface":            {"HF_TOKEN"},
	"fireworks":              {"FIREWORKS_API_KEY"},
	"together":               {"TOGETHER_API_KEY"},
	"opencode":               {"OPENCODE_API_KEY"},
	"opencode-go":            {"OPENCODE_API_KEY"},
	"kimi-coding":            {"KIMI_API_KEY"},
	"cloudflare-workers-ai":  {"CLOUDFLARE_API_KEY"},
	"cloudflare-ai-gateway":  {"CLOUDFLARE_API_KEY"},
	"xiaomi":                 {"XIAOMI_API_KEY"},
	"xiaomi-token-plan-cn":   {"XIAOMI_TOKEN_PLAN_CN_API_KEY"},
	"xiaomi-token-plan-ams":  {"XIAOMI_TOKEN_PLAN_AMS_API_KEY"},
	"xiaomi-token-plan-sgp":  {"XIAOMI_TOKEN_PLAN_SGP_API_KEY"},
}

func FindEnvKeys(provider Provider) []string {
	envVars := apiKeyEnvVars[string(provider)]
	if len(envVars) == 0 {
		return nil
	}
	found := []string{}
	for _, envVar := range envVars {
		if os.Getenv(envVar) != "" {
			found = append(found, envVar)
		}
	}
	if len(found) == 0 {
		return nil
	}
	return found
}

func GetEnvAPIKey(provider Provider) string {
	envKeys := FindEnvKeys(provider)
	if len(envKeys) > 0 {
		return os.Getenv(envKeys[0])
	}
	if provider == ProviderGoogleVertex && hasVertexADCCredentials() && hasGoogleCloudProjectAndLocation() {
		return "<authenticated>"
	}
	if provider == ProviderAmazonBedrock && hasAmazonBedrockCredentials() {
		return "<authenticated>"
	}
	return ""
}

func hasVertexADCCredentials() bool {
	if gacPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); gacPath != "" {
		return fileExists(gacPath)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	return fileExists(filepath.Join(home, ".config", "gcloud", "application_default_credentials.json"))
}

func hasGoogleCloudProjectAndLocation() bool {
	hasProject := os.Getenv("GOOGLE_CLOUD_PROJECT") != "" || os.Getenv("GCLOUD_PROJECT") != ""
	hasLocation := os.Getenv("GOOGLE_CLOUD_LOCATION") != ""
	return hasProject && hasLocation
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func hasAmazonBedrockCredentials() bool {
	if os.Getenv("AWS_PROFILE") != "" {
		return true
	}
	if os.Getenv("AWS_ACCESS_KEY_ID") != "" && os.Getenv("AWS_SECRET_ACCESS_KEY") != "" {
		return true
	}
	for _, envVar := range []string{
		"AWS_BEARER_TOKEN_BEDROCK",
		"AWS_CONTAINER_CREDENTIALS_RELATIVE_URI",
		"AWS_CONTAINER_CREDENTIALS_FULL_URI",
		"AWS_WEB_IDENTITY_TOKEN_FILE",
	} {
		if os.Getenv(envVar) != "" {
			return true
		}
	}
	return false
}
