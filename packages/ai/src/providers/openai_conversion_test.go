package providers

import (
	"reflect"
	"testing"

	ai "github.com/earendil-works/pi-mono/packages/ai/src"
)

func TestConvertCompletionsMessagesDowngradesUnsupportedImagesAndSynthesizesToolResults(t *testing.T) {
	model := ai.Model{ID: "gpt-test", API: ai.ApiOpenAICompletions, Provider: "openai", Input: []string{"text"}}
	messages := ConvertCompletionsMessages(model, ai.Context{
		Messages: []ai.Message{
			{Role: "user", Content: []ai.ContentBlock{{Type: "image", MimeType: "image/png", Data: "abc"}}},
			{Role: "assistant", API: ai.ApiOpenAIResponses, Provider: "openai", Model: "other", StopReason: ai.StopReasonToolUse, Content: []ai.ContentBlock{{Type: "toolCall", ID: "call/1|fc_very_long_item", Name: "read", Arguments: map[string]any{"path": "x"}}}},
		},
	})
	if got := messages[0]["content"].([]map[string]any)[0]["text"]; got != nonVisionUserImagePlaceholder {
		t.Fatalf("expected image placeholder, got %#v", got)
	}
	if messages[1]["role"] != "assistant" {
		t.Fatalf("expected assistant replay, got %#v", messages[1])
	}
	if messages[2]["role"] != "tool" {
		t.Fatalf("expected synthetic tool result, got %#v", messages[2])
	}
	if messages[2]["tool_call_id"] != "call_1" {
		t.Fatalf("expected normalized synthetic tool call id, got %#v", messages[2]["tool_call_id"])
	}
}

func TestConvertMessagesAliasesCompletionsConverter(t *testing.T) {
	model := ai.Model{ID: "gpt-test", API: ai.ApiOpenAICompletions, Provider: "openai", Input: []string{"text"}}
	context := ai.Context{Messages: []ai.Message{{Role: "user", Content: "hello"}}}
	got := ConvertMessages(model, context)
	want := ConvertCompletionsMessages(model, context)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected ConvertMessages alias to match completions converter\n got: %#v\nwant: %#v", got, want)
	}
}

func TestConvertCompletionsMessagesSkipsErroredAssistant(t *testing.T) {
	model := ai.Model{ID: "gpt-test", API: ai.ApiOpenAICompletions, Provider: "openai", Input: []string{"text"}}
	messages := ConvertCompletionsMessages(model, ai.Context{
		Messages: []ai.Message{
			{Role: "assistant", API: ai.ApiOpenAICompletions, Provider: "openai", Model: "gpt-test", StopReason: ai.StopReasonError, Content: []ai.ContentBlock{{Type: "text", Text: "partial"}}},
			{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "retry"}}},
		},
	})
	if len(messages) != 1 || messages[0]["role"] != "user" {
		t.Fatalf("expected only user message, got %#v", messages)
	}
}

func TestOpenAIConversionsSanitizeUnpairedSurrogates(t *testing.T) {
	text := string([]rune{'o', 'k', 0xD83D})
	completions := ConvertCompletionsMessages(ai.Model{ID: "gpt-test", API: ai.ApiOpenAICompletions, Provider: "openai"}, ai.Context{
		SystemPrompt: text,
		Messages:     []ai.Message{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: text}}}},
	})
	if completions[0]["content"] != "ok" {
		t.Fatalf("expected sanitized system prompt, got %#v", completions[0]["content"])
	}
	content := completions[1]["content"].([]map[string]any)
	if content[0]["text"] != "ok" {
		t.Fatalf("expected sanitized user text, got %#v", content[0]["text"])
	}
	responses := ConvertResponsesMessages(ai.Model{ID: "gpt-test", API: ai.ApiOpenAIResponses, Provider: "openai"}, ai.Context{
		SystemPrompt: text,
		Messages:     []ai.Message{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: text}}}},
	})
	if responses[0]["content"] != "ok" {
		t.Fatalf("expected sanitized responses system prompt, got %#v", responses[0]["content"])
	}
	responseContent := responses[1]["content"].([]map[string]any)
	if responseContent[0]["text"] != "ok" {
		t.Fatalf("expected sanitized responses user text, got %#v", responseContent[0]["text"])
	}
	toolResponses := ConvertResponsesMessages(ai.Model{ID: "gpt-test", API: ai.ApiOpenAIResponses, Provider: "openai", Input: []string{"text"}}, ai.Context{
		Messages: []ai.Message{{Role: "toolResult", ToolCallID: "call_1", Content: []ai.ContentBlock{{Type: "text", Text: text}}}},
	})
	if toolResponses[0]["output"] != "ok" {
		t.Fatalf("expected sanitized responses tool output, got %#v", toolResponses[0]["output"])
	}
}

func TestConvertResponsesMessagesSkipsEmptyStructuredUserContent(t *testing.T) {
	model := ai.Model{ID: "gpt-test", API: ai.ApiOpenAIResponses, Provider: "openai", Input: []string{"text"}}
	messages := ConvertResponsesMessages(model, ai.Context{
		Messages: []ai.Message{{Role: "user", Content: []ai.ContentBlock{}}},
	})
	if len(messages) != 0 {
		t.Fatalf("expected empty structured user message to be skipped, got %#v", messages)
	}
}

func TestConvertResponsesMessagesSplitsToolCallIDAndPreservesImages(t *testing.T) {
	model := ai.Model{ID: "gpt-test", API: ai.ApiOpenAIResponses, Provider: "openai", Input: []string{"text", "image"}}
	messages := ConvertResponsesMessages(model, ai.Context{
		Messages: []ai.Message{
			{Role: "assistant", API: ai.ApiOpenAIResponses, Provider: "openai", Model: "gpt-test", StopReason: ai.StopReasonToolUse, Content: []ai.ContentBlock{{Type: "toolCall", ID: "call_1|fc_item_1", Name: "inspect", Arguments: map[string]any{"x": 1}}}},
			{Role: "toolResult", ToolCallID: "call_1|fc_item_1", Content: []ai.ContentBlock{{Type: "text", Text: "look"}, {Type: "image", MimeType: "image/png", Data: "abc"}}},
		},
	})
	call := messages[0]
	if call["call_id"] != "call_1" || call["id"] != "fc_item_1" {
		t.Fatalf("expected split call/id, got %#v", call)
	}
	result := messages[1]
	if result["call_id"] != "call_1" {
		t.Fatalf("expected tool result call id without item id, got %#v", result["call_id"])
	}
	output, ok := result["output"].([]map[string]any)
	if !ok || len(output) != 2 || output[1]["type"] != "input_image" {
		t.Fatalf("expected text+image function output, got %#v", result["output"])
	}
}

func TestConvertResponsesMessagesOptions(t *testing.T) {
	includeSystemPrompt := false
	model := ai.Model{ID: "gpt-test", API: ai.ApiOpenAIResponses, Provider: "openai", Input: []string{"text"}}
	messages := ConvertResponsesMessages(model, ai.Context{
		SystemPrompt: "system",
		Messages:     []ai.Message{{Role: "user", Content: "hello"}},
	}, ConvertResponsesMessagesOptions{IncludeSystemPrompt: &includeSystemPrompt})
	if len(messages) != 1 || messages[0]["role"] != "user" {
		t.Fatalf("expected system prompt omitted, got %#v", messages)
	}
}

func TestConvertResponsesToolsStrictOption(t *testing.T) {
	strict := true
	tools := ConvertResponsesTools([]ai.Tool{{Name: "read", Description: "read", Parameters: map[string]any{"type": "object"}}}, ConvertResponsesToolsOptions{Strict: &strict})
	if len(tools) != 1 || tools[0]["strict"] != true {
		t.Fatalf("expected strict tool, got %#v", tools)
	}
}

func TestConvertResponsesMessagesDropsDifferentModelFunctionItemID(t *testing.T) {
	model := ai.Model{ID: "gpt-test", API: ai.ApiOpenAIResponses, Provider: "openai", Input: []string{"text"}}
	messages := ConvertResponsesMessages(model, ai.Context{
		Messages: []ai.Message{
			{
				Role:       "assistant",
				API:        ai.ApiOpenAIResponses,
				Provider:   "openai",
				Model:      "gpt-other",
				StopReason: ai.StopReasonToolUse,
				Content:    []ai.ContentBlock{{Type: "toolCall", ID: "call_1|fc_item_1", Name: "inspect", Arguments: map[string]any{"x": 1}}},
			},
		},
	})
	if _, ok := messages[0]["id"]; ok {
		t.Fatalf("expected different-model fc id to be omitted, got %#v", messages[0])
	}
}

func TestConvertResponsesMessagesPreservesTextPhase(t *testing.T) {
	model := ai.Model{ID: "gpt-test", API: ai.ApiOpenAIResponses, Provider: "openai", Input: []string{"text"}}
	messages := ConvertResponsesMessages(model, ai.Context{
		Messages: []ai.Message{
			{
				Role:     "assistant",
				API:      ai.ApiOpenAIResponses,
				Provider: "openai",
				Model:    "gpt-test",
				Content:  []ai.ContentBlock{{Type: "text", Text: "ok", TextSignature: `{"v":1,"id":"msg_1","phase":"final_answer"}`}},
			},
		},
	})
	if messages[0]["phase"] != "final_answer" {
		t.Fatalf("expected phase to be preserved, got %#v", messages[0])
	}
}

func TestBuildCompletionsParamsUsesDetectedCompat(t *testing.T) {
	maxTokens := 123
	reasoning := ai.ThinkingLevelHigh
	model := ai.Model{
		ID:        "deepseek-reasoner",
		API:       ai.ApiOpenAICompletions,
		Provider:  "deepseek",
		BaseURL:   "https://api.deepseek.com/v1",
		Reasoning: true,
		Input:     []string{"text"},
	}
	params := BuildCompletionsParams(model, ai.Context{}, OpenAICompletionsOptions{
		StreamOptions:   ai.StreamOptions{MaxTokens: &maxTokens},
		ReasoningEffort: &reasoning,
	})
	if _, ok := params["store"]; ok {
		t.Fatalf("deepseek should not send store, got %#v", params["store"])
	}
	if params["max_completion_tokens"] != maxTokens {
		t.Fatalf("expected max_completion_tokens, got %#v", params)
	}
	if thinking := params["thinking"].(map[string]any); thinking["type"] != "enabled" {
		t.Fatalf("expected deepseek thinking enabled, got %#v", thinking)
	}
	if params["reasoning_effort"] != "high" {
		t.Fatalf("expected reasoning_effort high, got %#v", params["reasoning_effort"])
	}
}

func TestRegisterBuiltInsIncludesOpenAICodexResponses(t *testing.T) {
	ResetAPIProviders()
	provider, ok := ai.GetAPIProvider(ai.ApiOpenAICodexResponses)
	if !ok {
		t.Fatal("expected openai-codex-responses provider")
	}
	if provider.API != ai.ApiOpenAICodexResponses || provider.Stream == nil || provider.StreamSimple == nil {
		t.Fatalf("unexpected codex provider %#v", provider)
	}
}

func TestSimpleReasoningEffortClampsAndOmitsOff(t *testing.T) {
	off := "none"
	xhigh := "extra"
	model := ai.Model{
		ID:        "reasoning-model",
		Reasoning: true,
		ThinkingLevelMap: map[ai.ModelThinkingLevel]*string{
			ai.ModelThinkingLevelOff:    &off,
			ai.ModelThinkingLevelMedium: nil,
			ai.ModelThinkingLevelHigh:   nil,
			ai.ModelThinkingLevelXHigh:  &xhigh,
		},
	}
	requestedMedium := ai.ThinkingLevelMedium
	got := simpleReasoningEffort(model, &requestedMedium)
	if got == nil || *got != ai.ThinkingLevelXHigh {
		t.Fatalf("expected medium to clamp to xhigh, got %#v", got)
	}
	requestedOff := ai.ThinkingLevel("off")
	if got := simpleReasoningEffort(model, &requestedOff); got != nil {
		t.Fatalf("expected off to be omitted, got %#v", got)
	}
	requestedHigh := ai.ThinkingLevelHigh
	nonReasoning := ai.Model{}
	if got := simpleReasoningEffort(nonReasoning, &requestedHigh); got != nil {
		t.Fatalf("expected non-reasoning model to omit effort, got %#v", got)
	}
}

func TestOpenAIClientConfigResolvesCloudflareGatewayHeaders(t *testing.T) {
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct")
	t.Setenv("CLOUDFLARE_GATEWAY_ID", "gateway")
	config, err := buildOpenAIClientConfig(ai.Model{
		API:      ai.ApiOpenAICompletions,
		Provider: ai.ProviderCloudflareAIGateway,
		BaseURL:  "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/compat",
		Headers:  map[string]string{"x-model": "1"},
	}, ai.StreamOptions{
		APIKey:    "gateway-key",
		SessionID: "session-1",
		Headers:   map[string]string{"x-extra": "2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.BaseURL != "https://gateway.ai.cloudflare.com/v1/acct/gateway/compat" {
		t.Fatalf("unexpected base URL %q", config.BaseURL)
	}
	if config.Headers["cf-aig-authorization"] != "Bearer gateway-key" {
		t.Fatalf("expected Cloudflare auth header, got %#v", config.Headers)
	}
	if authorization, ok := config.Headers["Authorization"]; !ok || authorization != "" {
		t.Fatalf("expected blank Authorization override, got %#v", config.Headers)
	}
	if config.Headers["x-model"] != "1" || config.Headers["x-extra"] != "2" {
		t.Fatalf("expected merged headers, got %#v", config.Headers)
	}
	if config.Headers["x-client-request-id"] != "session-1" {
		t.Fatalf("expected request id header, got %#v", config.Headers)
	}
}

func TestResolveCloudflareBaseURLReportsMissingEnv(t *testing.T) {
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "")
	_, err := ResolveCloudflareBaseURL(ai.Model{
		Provider: ai.ProviderCloudflareAIGateway,
		BaseURL:  "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/compat",
	})
	if err == nil || err.Error() != "CLOUDFLARE_ACCOUNT_ID is required for provider cloudflare-ai-gateway but is not set." {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestOpenAIPayloadReplacementRejectsUnsupportedBody(t *testing.T) {
	model := ai.Model{ID: "gpt-test", API: ai.ApiOpenAICompletions, Provider: ai.ProviderOpenAI}
	completions := StreamOpenAICompletions(t.Context(), model, ai.Context{}, OpenAICompletionsOptions{
		StreamOptions: ai.StreamOptions{APIKey: "key", OnPayload: func(payload any, model ai.Model) (any, bool, error) {
			return "bad", true, nil
		}},
	}).Result()
	if completions.StopReason != ai.StopReasonError || completions.ErrorMessage != "onPayload returned unsupported OpenAI request body" {
		t.Fatalf("unexpected completions result %#v", completions)
	}

	responses := StreamOpenAIResponses(t.Context(), ai.Model{ID: "gpt-test", API: ai.ApiOpenAIResponses, Provider: ai.ProviderOpenAI}, ai.Context{}, OpenAIResponsesOptions{
		StreamOptions: ai.StreamOptions{APIKey: "key", OnPayload: func(payload any, model ai.Model) (any, bool, error) {
			return []string{"bad"}, true, nil
		}},
	}).Result()
	if responses.StopReason != ai.StopReasonError || responses.ErrorMessage != "onPayload returned unsupported OpenAI request body" {
		t.Fatalf("unexpected responses result %#v", responses)
	}
}

func TestBuildCompletionsParamsSupportsTogetherCompat(t *testing.T) {
	maxTokens := 456
	reasoning := ai.ThinkingLevelLow
	model := ai.Model{
		ID:        "meta-llama/test",
		API:       ai.ApiOpenAICompletions,
		Provider:  "together",
		BaseURL:   "https://api.together.ai/v1",
		Reasoning: true,
		Input:     []string{"text"},
	}
	params := BuildCompletionsParams(model, ai.Context{}, OpenAICompletionsOptions{
		StreamOptions:   ai.StreamOptions{MaxTokens: &maxTokens, SessionID: "session-1", CacheRetention: ai.CacheRetentionLong},
		ReasoningEffort: &reasoning,
	})
	if params["max_tokens"] != maxTokens {
		t.Fatalf("expected together max_tokens, got %#v", params)
	}
	if _, ok := params["prompt_cache_retention"]; ok {
		t.Fatalf("together should not send long cache retention, got %#v", params["prompt_cache_retention"])
	}
	if reasoningPayload := params["reasoning"].(map[string]any); reasoningPayload["enabled"] != true {
		t.Fatalf("expected together reasoning enabled, got %#v", reasoningPayload)
	}
	if _, ok := params["reasoning_effort"]; ok {
		t.Fatalf("together should omit reasoning_effort by detected compat, got %#v", params["reasoning_effort"])
	}
}

func TestBuildCompletionsParamsHonorsExplicitCompatOverrides(t *testing.T) {
	model := ai.Model{
		ID:       "custom",
		API:      ai.ApiOpenAICompletions,
		Provider: "custom",
		BaseURL:  "https://example.com/v1",
		Input:    []string{"text"},
		Compat: map[string]any{
			"supportsStore":                    false,
			"supportsUsageInStreaming":         false,
			"maxTokensField":                   "max_tokens",
			"supportsStrictMode":               false,
			"requiresAssistantAfterToolResult": true,
			"requiresToolResultName":           true,
		},
	}
	maxTokens := 10
	params := BuildCompletionsParams(model, ai.Context{
		Messages: []ai.Message{
			{Role: "toolResult", ToolCallID: "call_1", ToolName: "tool", Content: []ai.ContentBlock{{Type: "text", Text: "ok"}}},
			{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "next"}}},
		},
		Tools: []ai.Tool{{Name: "tool", Description: "tool", Parameters: map[string]any{"type": "object"}}},
	}, OpenAICompletionsOptions{StreamOptions: ai.StreamOptions{MaxTokens: &maxTokens}})
	if _, ok := params["store"]; ok {
		t.Fatalf("supportsStore false should omit store")
	}
	if _, ok := params["stream_options"]; ok {
		t.Fatalf("supportsUsageInStreaming false should omit stream_options")
	}
	if params["max_tokens"] != maxTokens {
		t.Fatalf("expected max_tokens override, got %#v", params)
	}
	tool := params["tools"].([]map[string]any)[0]["function"].(map[string]any)
	if _, ok := tool["strict"]; ok {
		t.Fatalf("supportsStrictMode false should omit strict")
	}
	messages := params["messages"].([]map[string]any)
	if messages[0]["name"] != "tool" {
		t.Fatalf("requiresToolResultName should include name, got %#v", messages[0])
	}
	if messages[1]["role"] != "assistant" {
		t.Fatalf("requiresAssistantAfterToolResult should bridge before user, got %#v", messages)
	}
}

func TestBuildCompletionsParamsAppliesAnthropicCacheControl(t *testing.T) {
	model := ai.Model{
		ID:        "anthropic/claude-test",
		API:       ai.ApiOpenAICompletions,
		Provider:  "openrouter",
		BaseURL:   "https://openrouter.ai/api/v1",
		Input:     []string{"text"},
		Reasoning: false,
	}
	params := BuildCompletionsParams(model, ai.Context{
		SystemPrompt: "system",
		Messages: []ai.Message{
			{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "hello"}}},
		},
		Tools: []ai.Tool{{Name: "read", Description: "read", Parameters: map[string]any{"type": "object"}}},
	}, OpenAICompletionsOptions{StreamOptions: ai.StreamOptions{SessionID: "session-1", CacheRetention: ai.CacheRetentionLong}})

	messages := params["messages"].([]map[string]any)
	systemContent := messages[0]["content"].([]map[string]any)
	systemCache := systemContent[0]["cache_control"].(map[string]any)
	if systemCache["type"] != "ephemeral" || systemCache["ttl"] != "1h" {
		t.Fatalf("expected long cache control on system prompt, got %#v", systemCache)
	}
	userContent := messages[1]["content"].([]map[string]any)
	if userContent[0]["cache_control"].(map[string]any)["ttl"] != "1h" {
		t.Fatalf("expected cache control on last user message, got %#v", userContent)
	}
	tools := params["tools"].([]map[string]any)
	if tools[0]["cache_control"].(map[string]any)["ttl"] != "1h" {
		t.Fatalf("expected cache control on last tool, got %#v", tools[0])
	}
}

func TestBuildResponsesParamsUsesCacheAndOffReasoningCompat(t *testing.T) {
	off := "none"
	model := ai.Model{
		ID:        "gpt-test",
		API:       ai.ApiOpenAIResponses,
		Provider:  "openai",
		BaseURL:   "https://api.openai.com/v1",
		Reasoning: true,
		Input:     []string{"text"},
		ThinkingLevelMap: map[ai.ModelThinkingLevel]*string{
			ai.ModelThinkingLevelOff: &off,
		},
	}
	params := BuildResponsesParams(model, ai.Context{}, OpenAIResponsesOptions{
		StreamOptions: ai.StreamOptions{SessionID: "session-1", CacheRetention: ai.CacheRetentionLong},
	})
	if params["prompt_cache_key"] != "session-1" {
		t.Fatalf("expected prompt cache key, got %#v", params["prompt_cache_key"])
	}
	if params["prompt_cache_retention"] != "24h" {
		t.Fatalf("expected long cache retention, got %#v", params["prompt_cache_retention"])
	}
	if reasoning := params["reasoning"].(map[string]any); reasoning["effort"] != "none" {
		t.Fatalf("expected off reasoning effort, got %#v", reasoning)
	}
}
