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
const defaultTitleGenerationModel = "gpt-5-mini"
const openRouterTitleGenerationModel = "openai/gpt-5-mini"

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
		c.DefaultSystemPrompt = "You are a helpful assistant inside Beeper."
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
