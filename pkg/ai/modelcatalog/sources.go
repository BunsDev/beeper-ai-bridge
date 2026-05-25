package modelcatalog

import (
	"encoding/json"
	"strconv"
)

type modelsDevResponse map[string]modelsDevProvider

type modelsDevProvider struct {
	Models map[string]modelsDevModel `json:"models"`
}

type modelsDevModel struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Reasoning  bool                `json:"reasoning"`
	ToolCall   bool                `json:"tool_call"`
	Modalities modelsDevModalities `json:"modalities"`
	Cost       modelsDevCost       `json:"cost"`
	Limit      modelsDevLimit      `json:"limit"`
}

type modelsDevModalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

type modelsDevCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

type modelsDevLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

type openRouterResponse struct {
	Data []openRouterModel `json:"data"`
}

type openRouterModel struct {
	ID                  string                 `json:"id"`
	Name                string                 `json:"name"`
	SupportedParameters []string               `json:"supported_parameters"`
	Architecture        openRouterArchitecture `json:"architecture"`
	Pricing             openRouterPricing      `json:"pricing"`
	ContextLength       int                    `json:"context_length"`
	TopProvider         openRouterTopProvider  `json:"top_provider"`
}

type openRouterArchitecture struct {
	InputModalities []string `json:"input_modalities"`
}

type openRouterPricing struct {
	Prompt          float64 `json:"prompt"`
	Completion      float64 `json:"completion"`
	InputCacheRead  float64 `json:"input_cache_read"`
	InputCacheWrite float64 `json:"input_cache_write"`
}

type openRouterTopProvider struct {
	ContextLength       int `json:"context_length"`
	MaxCompletionTokens int `json:"max_completion_tokens"`
}

func (pricing *openRouterPricing) UnmarshalJSON(data []byte) error {
	type rawPricing struct {
		Prompt          number `json:"prompt"`
		Completion      number `json:"completion"`
		InputCacheRead  number `json:"input_cache_read"`
		InputCacheWrite number `json:"input_cache_write"`
	}
	var raw rawPricing
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	pricing.Prompt = float64(raw.Prompt)
	pricing.Completion = float64(raw.Completion)
	pricing.InputCacheRead = float64(raw.InputCacheRead)
	pricing.InputCacheWrite = float64(raw.InputCacheWrite)
	return nil
}

type number float64

func (n *number) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*n = 0
		return nil
	}
	if data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		if text == "" {
			*n = 0
			return nil
		}
		value, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return err
		}
		*n = number(value)
		return nil
	}
	var value float64
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*n = number(value)
	return nil
}
