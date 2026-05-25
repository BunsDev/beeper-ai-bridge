package connector

import (
	_ "embed"
	"strings"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
	up "go.mau.fi/util/configupgrade"
	"gopkg.in/yaml.v3"
)

//go:embed example-config.yaml
var ExampleConfig string

type Config struct {
	BeeperEnvTLD          string                         `yaml:"beeper_env_tld"`
	DefaultProvider       DefaultProviderConfig          `yaml:"default_provider"`
	Providers             map[string]aiid.ProviderConfig `yaml:"providers"`
	DefaultSystemPrompt   string                         `yaml:"default_system_prompt"`
	DefaultReasoningLevel string                         `yaml:"default_reasoning_level"`
	Fetch                 FetchConfig                    `yaml:"fetch"`
	Search                SearchConfig                   `yaml:"search"`
	StreamType            string                         `yaml:"stream_type"`
}

type DefaultProviderConfig struct {
	BaseURL       string      `yaml:"base_url"`
	Provider      ai.Provider `yaml:"provider"`
	API           ai.Api      `yaml:"api"`
	DefaultModel  string      `yaml:"default_model"`
	AllowedModels []string    `yaml:"allowed_models"`
	Models        []ai.Model  `yaml:"models"`
}

type FetchConfig struct {
	TimeoutMS int   `yaml:"timeout_ms"`
	MaxBytes  int64 `yaml:"max_bytes"`
	MaxChars  int   `yaml:"max_chars"`
}

type SearchConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Endpoint string `yaml:"endpoint"`
	APIKey   string `yaml:"api_key"`
}

type umConfig Config

func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	if err := node.Decode((*umConfig)(c)); err != nil {
		return err
	}
	c.ApplyDefaults()
	return nil
}

func (c *Config) ApplyDefaults() {
	if c.BeeperEnvTLD == "" {
		c.BeeperEnvTLD = "beeper.com"
	}
	if c.DefaultProvider.BaseURL == "" {
		c.DefaultProvider.BaseURL = "https://ai-proxy." + c.BeeperEnvTLD + "/v1/responses"
	}
	if c.StreamType == "" {
		c.StreamType = aiid.StreamType
	}
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
	if c.DefaultProvider.Provider == "" {
		c.DefaultProvider.Provider = ai.ProviderOpenAI
	}
	if c.DefaultProvider.API == "" {
		c.DefaultProvider.API = ai.ApiOpenAIResponses
	}
	if c.DefaultProvider.DefaultModel == "" {
		c.DefaultProvider.DefaultModel = "gpt-5.5"
	}
	if len(c.DefaultProvider.AllowedModels) == 0 && len(c.DefaultProvider.Models) == 0 {
		c.DefaultProvider.AllowedModels = []string{c.DefaultProvider.DefaultModel, "gpt-5.4", "gpt-5"}
	}
	for i := range c.DefaultProvider.Models {
		c.DefaultProvider.Models[i] = normalizeDefaultModel(c.DefaultProvider.Models[i], c.DefaultProvider.BaseURL)
	}
}

func normalizeDefaultModel(model ai.Model, baseURL string) ai.Model {
	if model.API == "" {
		model.API = ai.ApiOpenAIResponses
	}
	if model.Provider == "" {
		model.Provider = ai.ProviderOpenAI
	}
	if model.BaseURL == "" {
		model.BaseURL = normalizeResponsesBaseURL(baseURL)
	}
	if model.Name == "" {
		model.Name = model.ID
	}
	if len(model.Input) == 0 {
		model.Input = []string{"text"}
	}
	return model
}

func normalizeResponsesBaseURL(baseURL string) string {
	return strings.TrimSuffix(baseURL, "/responses")
}

func upgradeConfig(helper up.Helper) {
	helper.Copy(up.Str, "beeper_env_tld")
	helper.Copy(up.Map, "default_provider")
	helper.Copy(up.Map, "providers")
	helper.Copy(up.Str, "default_system_prompt")
	helper.Copy(up.Str, "default_reasoning_level")
	helper.Copy(up.Map, "fetch")
	helper.Copy(up.Map, "search")
	helper.Copy(up.Str, "stream_type")
}

func (c *Connector) GetConfig() (string, any, up.Upgrader) {
	c.Config.ApplyDefaults()
	return ExampleConfig, &c.Config, up.SimpleUpgrader(upgradeConfig)
}
