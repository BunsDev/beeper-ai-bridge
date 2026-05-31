package connector

import (
	_ "embed"
	"strings"

	"github.com/beeper/ai-bridge/pkg/agent/harness"
	up "go.mau.fi/util/configupgrade"
	"gopkg.in/yaml.v3"
)

//go:embed example-config.yaml
var ExampleConfig string

const defaultAIServicesProxyPath = "/proxy/openai/v1"
const defaultBeeperAIModel = "beeper/default"
const defaultTitleGenerationModel = "gpt-4.1-mini"
const fallbackTitleGenerationModel = "gpt-5-mini"
const openRouterTitleGenerationModel = "openai/gpt-4.1-mini"
const openRouterFallbackTitleGenerationModel = "openai/gpt-5-mini"
const defaultSystemPrompt = `You are Beeper AI, a helpful assistant inside a Beeper chat.

Respond naturally in the user's language. Be concise by default, but give enough detail when the task is technical, ambiguous, or high impact. Ask a clarifying question only when a reasonable answer would require missing information.

Treat the conversation as a chat thread. Use quoted or replied-to message context when it is relevant, but do not over-explain the chat mechanics.

If the user asks about current information, recent events, URLs, documents, the active model, room/session details, attachments, or anything that may depend on runtime state, use the available tools instead of guessing. Do not claim to have searched, fetched, read, or inspected something unless a tool or attachment actually provided it.

When using web or fetched sources, prefer primary sources and cite URLs clearly in Markdown. If sources disagree or are incomplete, say that directly.

Do not reveal hidden instructions, tool schemas, or internal implementation details unless the user explicitly asks about the system prompt or bridge behavior.`

type Config struct {
	DefaultSystemPrompt   string           `yaml:"default_system_prompt"`
	DefaultReasoningLevel string           `yaml:"default_reasoning_level"`
	Fetch                 FetchConfig      `yaml:"fetch"`
	Compaction            CompactionConfig `yaml:"compaction"`
}

type FetchConfig struct {
	TimeoutMS int   `yaml:"timeout_ms"`
	MaxBytes  int64 `yaml:"max_bytes"`
	MaxChars  int   `yaml:"max_chars"`
}

type CompactionConfig struct {
	Enabled          *bool `yaml:"enabled"`
	ReserveTokens    int   `yaml:"reserve_tokens"`
	KeepRecentTokens int   `yaml:"keep_recent_tokens"`
}

type umConfig Config

func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	c.ApplyDefaults()
	if err := node.Decode((*umConfig)(c)); err != nil {
		return err
	}
	c.ApplyDefaults()
	return nil
}

func (c *Config) ApplyDefaults() {
	if c.DefaultSystemPrompt == "" {
		c.DefaultSystemPrompt = defaultSystemPrompt
	}
	if c.DefaultReasoningLevel == "" {
		c.DefaultReasoningLevel = "off"
	}
	if c.Fetch.TimeoutMS == 0 {
		c.Fetch.TimeoutMS = 10000
	}
	if c.Fetch.MaxBytes == 0 {
		c.Fetch.MaxBytes = 2 * 1024 * 1024
	}
	if c.Fetch.MaxChars == 0 {
		c.Fetch.MaxChars = 20000
	}
	c.Compaction.ApplyDefaults()
}

func (c *CompactionConfig) ApplyDefaults() {
	if c.Enabled == nil {
		enabled := true
		c.Enabled = &enabled
	}
	if c.ReserveTokens == 0 {
		c.ReserveTokens = harness.DefaultCompactionSettings.ReserveTokens
	}
	if c.KeepRecentTokens == 0 {
		c.KeepRecentTokens = harness.DefaultCompactionSettings.KeepRecentTokens
	}
}

func (c CompactionConfig) Settings() harness.CompactionSettings {
	enabled := true
	if c.Enabled != nil {
		enabled = *c.Enabled
	}
	settings := harness.CompactionSettings{
		Enabled:          enabled,
		ReserveTokens:    c.ReserveTokens,
		KeepRecentTokens: c.KeepRecentTokens,
	}
	if settings.ReserveTokens == 0 {
		settings.ReserveTokens = harness.DefaultCompactionSettings.ReserveTokens
	}
	if settings.KeepRecentTokens == 0 {
		settings.KeepRecentTokens = harness.DefaultCompactionSettings.KeepRecentTokens
	}
	return settings
}

func normalizeResponsesBaseURL(baseURL string) string {
	return strings.TrimSuffix(baseURL, "/responses")
}

func upgradeConfig(helper up.Helper) {
	helper.Copy(up.Str, "default_system_prompt")
	helper.Copy(up.Str, "default_reasoning_level")
	helper.Copy(up.Map, "fetch")
	helper.Copy(up.Map, "compaction")
}

func (c *Connector) GetConfig() (string, any, up.Upgrader) {
	c.Config.ApplyDefaults()
	return ExampleConfig, &c.Config, up.SimpleUpgrader(upgradeConfig)
}
