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
	BeeperEnvTLD       string                `yaml:"beeper_env_tld"`
	DefaultProvider    DefaultProviderConfig `yaml:"default_provider"`
	RoomStateEventType string                `yaml:"room_state_event_type"`
	StreamType         string                `yaml:"stream_type"`
	Tools              ToolsConfig           `yaml:"tools"`
}

type DefaultProviderConfig struct {
	BaseURL string     `yaml:"base_url"`
	Models  []ai.Model `yaml:"models"`
}

type ToolsConfig struct {
	Enabled        bool     `yaml:"enabled"`
	WorkspaceRoots []string `yaml:"workspace_roots"`
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
		c.DefaultProvider.BaseURL = "https://ai-services." + c.BeeperEnvTLD + "/proxy/v1/responses"
	}
	if c.RoomStateEventType == "" {
		c.RoomStateEventType = aiid.RoomConfigType
	}
	if c.StreamType == "" {
		c.StreamType = aiid.StreamType
	}
	if len(c.DefaultProvider.Models) == 0 {
		c.DefaultProvider.Models = []ai.Model{{
			ID:            "gpt-5",
			Name:          "GPT-5",
			API:           ai.ApiOpenAIResponses,
			Provider:      ai.Provider(aiid.DefaultProvider),
			BaseURL:       normalizeResponsesBaseURL(c.DefaultProvider.BaseURL),
			Input:         []string{"text", "image"},
			ContextWindow: 400000,
			MaxTokens:     128000,
		}}
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
		model.Provider = ai.Provider(aiid.DefaultProvider)
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
	helper.Copy(up.Str, "room_state_event_type")
	helper.Copy(up.Str, "stream_type")
	helper.Copy(up.Map, "tools")
}

func (c *Connector) GetConfig() (string, any, up.Upgrader) {
	c.Config.ApplyDefaults()
	return ExampleConfig, &c.Config, up.SimpleUpgrader(upgradeConfig)
}
