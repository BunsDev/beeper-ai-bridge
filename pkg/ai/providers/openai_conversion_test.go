package providers

import (
	"context"
	"errors"
	"reflect"
	"testing"

	ai "github.com/beeper/ai-bridge/pkg/ai"
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

func TestCompleteOpenAICompletionsUsesNonStreamingPayload(t *testing.T) {
	stopErr := errors.New("stop after payload")
	called := false
	result := CompleteOpenAICompletions(context.Background(), ai.Model{
		ID:       "gpt-4.1-mini",
		API:      ai.ApiOpenAICompletions,
		Provider: ai.ProviderOpenAI,
	}, ai.Context{Messages: []ai.Message{{Role: "user", Content: "title"}}}, OpenAICompletionsOptions{
		StreamOptions: ai.StreamOptions{
			OnPayload: func(payload any, model ai.Model) (any, bool, error) {
				called = true
				body := payload.(map[string]any)
				if body["stream"] != false {
					t.Fatalf("expected non-streaming completion payload, got %#v", body["stream"])
				}
				if _, ok := body["stream_options"]; ok {
					t.Fatalf("did not expect stream_options in non-streaming completion payload: %#v", body)
				}
				return nil, false, stopErr
			},
		},
	})
	if !called {
		t.Fatal("expected payload hook")
	}
	if result.StopReason != ai.StopReasonError || result.ErrorMessage != stopErr.Error() {
		t.Fatalf("unexpected result %#v", result)
	}
}

func TestCompleteOpenAIResponsesUsesNonStreamingPayload(t *testing.T) {
	stopErr := errors.New("stop after payload")
	called := false
	result := CompleteOpenAIResponses(context.Background(), ai.Model{
		ID:       "gpt-5-mini",
		API:      ai.ApiOpenAIResponses,
		Provider: ai.ProviderOpenAI,
	}, ai.Context{Messages: []ai.Message{{Role: "user", Content: "title"}}}, OpenAIResponsesOptions{
		StreamOptions: ai.StreamOptions{
			OnPayload: func(payload any, model ai.Model) (any, bool, error) {
				called = true
				body := payload.(map[string]any)
				if body["stream"] != false {
					t.Fatalf("expected non-streaming responses payload, got %#v", body["stream"])
				}
				return nil, false, stopErr
			},
		},
	})
	if !called {
		t.Fatal("expected payload hook")
	}
	if result.StopReason != ai.StopReasonError || result.ErrorMessage != stopErr.Error() {
		t.Fatalf("unexpected result %#v", result)
	}
}

func TestCompleteOpenAIResponsesExtractsOpenRouterWebFetchSources(t *testing.T) {
	model := ai.Model{ID: "x-ai/grok-4.20", API: ai.ApiOpenAIResponses, Provider: ai.ProviderOpenRouter}
	output := newAssistant(model)
	applyCompleteOpenAIResponses(&output, model, OpenAIResponsesOptions{}, map[string]any{
		"id":     "resp_1",
		"status": "completed",
		"output": []any{map[string]any{
			"type":   "openrouter:web_fetch",
			"id":     "st_1",
			"status": "completed",
			"url":    "https://example.com/fetched",
			"title":  "Fetched Page",
		}, map[string]any{
			"type": "message",
			"id":   "msg_1",
			"content": []any{map[string]any{
				"type": "output_text",
				"text": "ok",
			}},
		}},
	})
	if len(output.Citations) != 1 || output.Citations[0].URL != "https://example.com/fetched" || output.Citations[0].Title != "Fetched Page" {
		t.Fatalf("expected OpenRouter fetch source from non-stream response, got %#v", output.Citations)
	}
}

func TestConvertCompletionsMessagesIncludesNativeAudio(t *testing.T) {
	model := ai.Model{ID: "gpt-audio", API: ai.ApiOpenAICompletions, Provider: "openai", Input: []string{"text", "audio"}}
	messages := ConvertCompletionsMessages(model, ai.Context{
		Messages: []ai.Message{{Role: "user", Content: []ai.ContentBlock{
			{Type: "text", Text: "listen to this"},
			{Type: "audio", MimeType: "audio/mpeg", Data: "abc123"},
		}}},
	})
	content := messages[0]["content"].([]map[string]any)
	audioPart := content[1]
	if audioPart["type"] != "input_audio" {
		t.Fatalf("expected native audio part, got %#v", audioPart)
	}
	audio := audioPart["input_audio"].(map[string]any)
	if audio["data"] != "abc123" || audio["format"] != "mp3" {
		t.Fatalf("unexpected audio payload %#v", audio)
	}
}

func TestProviderCitationsFromOpenAIAnnotations(t *testing.T) {
	citations := providerCitationsFromAny(map[string]any{
		"type": "message",
		"content": []any{map[string]any{
			"type": "output_text",
			"text": "hello citation",
			"annotations": []any{map[string]any{
				"type":        "url_citation",
				"start_index": float64(6),
				"end_index":   float64(14),
				"url":         "https://example.com/source",
				"title":       "Source",
			}},
		}},
	}, ai.ProviderOpenAI, 0)
	if len(citations) != 1 || citations[0].URL != "https://example.com/source" || citations[0].Title != "Source" {
		t.Fatalf("unexpected citations %#v", citations)
	}
	if citations[0].StartIndex == nil || *citations[0].StartIndex != 6 || citations[0].EndIndex == nil || *citations[0].EndIndex != 14 {
		t.Fatalf("missing citation range %#v", citations[0])
	}
}

func TestProviderCitationsFromGoogleGrounding(t *testing.T) {
	citations := providerCitationsFromAny(map[string]any{
		"candidates": []any{map[string]any{
			"groundingMetadata": map[string]any{
				"groundingChunks": []any{map[string]any{
					"web": map[string]any{"uri": "https://example.com/google", "title": "Google Source"},
				}},
				"groundingSupports": []any{map[string]any{
					"segment":               map[string]any{"startIndex": float64(2), "endIndex": float64(8), "text": "claim"},
					"groundingChunkIndices": []any{float64(0)},
				}},
			},
		}},
	}, ai.ProviderGoogle, 0)
	if len(citations) != 1 || citations[0].URL != "https://example.com/google" || citations[0].Title != "Google Source" {
		t.Fatalf("unexpected grounding citations %#v", citations)
	}
	if citations[0].StartIndex == nil || *citations[0].StartIndex != 2 || citations[0].EndIndex == nil || *citations[0].EndIndex != 8 || citations[0].Text != "claim" {
		t.Fatalf("missing grounding citation range %#v", citations[0])
	}
}

func TestProviderCitationsFromAnthropicWebSearchLocation(t *testing.T) {
	citations := providerCitationsFromAny(map[string]any{
		"type":       "web_search_result_location",
		"url":        "https://example.com/anthropic",
		"title":      "Anthropic Source",
		"cited_text": "quoted source text",
	}, ai.ProviderAnthropic, 0)
	if len(citations) != 1 || citations[0].URL != "https://example.com/anthropic" || citations[0].Title != "Anthropic Source" {
		t.Fatalf("unexpected citations %#v", citations)
	}
	if citations[0].Text != "quoted source text" {
		t.Fatalf("missing cited text %#v", citations[0])
	}
}

func TestProviderCitationsFromAnthropicWebFetchResult(t *testing.T) {
	citations := providerCitationsFromAny(map[string]any{
		"type": "web_fetch_tool_result",
		"content": map[string]any{
			"type": "web_fetch_result",
			"url":  "https://example.com/article",
			"content": map[string]any{
				"type":  "document",
				"title": "Fetched Article",
			},
		},
	}, ai.ProviderAnthropic, 0)
	if len(citations) != 1 || citations[0].URL != "https://example.com/article" || citations[0].Title != "Fetched Article" || citations[0].RawType != "web_fetch_result" {
		t.Fatalf("unexpected web fetch citations %#v", citations)
	}
}

func TestProviderCitationsFromOpenRouterWebFetchItem(t *testing.T) {
	citations := providerCitationsFromAny(map[string]any{
		"type":       "openrouter:web_fetch",
		"status":     "completed",
		"url":        "https://example.com/openrouter-fetch",
		"title":      "OpenRouter Fetch",
		"httpStatus": float64(200),
		"content":    "Fetched page text",
	}, ai.ProviderOpenRouter, 0)
	if len(citations) != 1 || citations[0].URL != "https://example.com/openrouter-fetch" || citations[0].Title != "OpenRouter Fetch" || citations[0].RawType != "openrouter:web_fetch" {
		t.Fatalf("unexpected OpenRouter web fetch citations %#v", citations)
	}
}

func TestProviderCitationsFromGoogleURLContextMetadata(t *testing.T) {
	citations := providerCitationsFromAny(map[string]any{
		"candidates": []any{map[string]any{
			"urlContextMetadata": map[string]any{
				"urlMetadata": []any{map[string]any{
					"retrievedUrl":       "https://example.com/url-context",
					"urlRetrievalStatus": "URL_RETRIEVAL_STATUS_SUCCESS",
				}},
			},
		}},
	}, ai.ProviderGoogle, 0)
	if len(citations) != 1 || citations[0].URL != "https://example.com/url-context" || citations[0].RawType != "url_context" {
		t.Fatalf("unexpected URL context citations %#v", citations)
	}
}

func TestConvertResponsesMessagesIncludesNativeAudio(t *testing.T) {
	model := ai.Model{ID: "gpt-audio", API: ai.ApiOpenAIResponses, Provider: "openai", Input: []string{"text", "audio"}}
	messages := ConvertResponsesMessages(model, ai.Context{
		Messages: []ai.Message{{Role: "user", Content: []ai.ContentBlock{
			{Type: "text", Text: "listen to this"},
			{Type: "audio", MimeType: "audio/wav", Data: "abc123"},
		}}},
	})
	content := messages[0]["content"].([]map[string]any)
	audioPart := content[1]
	if audioPart["type"] != "input_audio" {
		t.Fatalf("expected native audio part, got %#v", audioPart)
	}
	audio := audioPart["input_audio"].(map[string]any)
	if audio["data"] != "abc123" || audio["format"] != "wav" {
		t.Fatalf("unexpected audio payload %#v", audio)
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

func TestConvertResponsesMessagesDropsReasoningIDWithoutEncryptedContent(t *testing.T) {
	model := ai.Model{ID: "gpt-test", API: ai.ApiOpenAIResponses, Provider: "openai", Input: []string{"text"}}
	messages := ConvertResponsesMessages(model, ai.Context{
		Messages: []ai.Message{
			{
				Role:     "assistant",
				API:      ai.ApiOpenAIResponses,
				Provider: "openai",
				Model:    "gpt-test",
				Content: []ai.ContentBlock{
					{Type: "thinking", ThinkingSignature: `{"type":"reasoning","id":"rs_123","summary":[]}`},
					{Type: "text", Text: "ok", TextSignature: `{"v":1,"id":"msg_1"}`},
				},
			},
		},
	})
	if len(messages) != 1 || messages[0]["type"] != "message" {
		t.Fatalf("expected non-replayable reasoning item to be omitted, got %#v", messages)
	}
}

func TestConvertResponsesMessagesPreservesReasoningWithEncryptedContent(t *testing.T) {
	model := ai.Model{ID: "gpt-test", API: ai.ApiOpenAIResponses, Provider: "openai", Input: []string{"text"}}
	messages := ConvertResponsesMessages(model, ai.Context{
		Messages: []ai.Message{
			{
				Role:     "assistant",
				API:      ai.ApiOpenAIResponses,
				Provider: "openai",
				Model:    "gpt-test",
				Content: []ai.ContentBlock{
					{Type: "thinking", ThinkingSignature: `{"type":"reasoning","id":"rs_123","encrypted_content":"ciphertext"}`},
					{Type: "text", Text: "ok", TextSignature: `{"v":1,"id":"msg_1"}`},
				},
			},
		},
	})
	if len(messages) != 2 || messages[0]["type"] != "reasoning" {
		t.Fatalf("expected replayable reasoning item to be preserved, got %#v", messages)
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

func TestModelRegistryOnlyExposesRegisteredTextAPIs(t *testing.T) {
	ResetAPIProviders()
	for _, provider := range ai.GetProviders() {
		for _, model := range ai.GetModels(provider) {
			if _, ok := ai.GetAPIProvider(model.API); !ok {
				t.Fatalf("model %s/%s uses unregistered api %s", provider, model.ID, model.API)
			}
		}
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
	}, ai.Context{}, ai.StreamOptions{
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
	if _, ok := config.Headers["x-client-request-id"]; ok {
		t.Fatalf("default completions compat should not send request id header, got %#v", config.Headers)
	}
}

func TestOpenAIClientConfigResponsesHonorsSessionIDCompat(t *testing.T) {
	config, err := buildOpenAIClientConfig(ai.Model{
		API:      ai.ApiOpenAIResponses,
		Provider: ai.ProviderOpenAI,
		BaseURL:  "https://api.openai.com/v1",
		Compat:   map[string]any{"sendSessionIdHeader": false},
	}, ai.Context{}, ai.StreamOptions{APIKey: "token", SessionID: "session-1"})
	if err != nil {
		t.Fatal(err)
	}
	if config.Headers["x-client-request-id"] != "session-1" {
		t.Fatalf("expected request id header, got %#v", config.Headers)
	}
	if _, ok := config.Headers["session_id"]; ok {
		t.Fatalf("sendSessionIdHeader=false should suppress session_id, got %#v", config.Headers)
	}
}

func TestOpenAIClientConfigCompletionsAffinityHeadersRequireCompat(t *testing.T) {
	defaultConfig, err := buildOpenAIClientConfig(ai.Model{
		API:      ai.ApiOpenAICompletions,
		Provider: ai.ProviderOpenRouter,
		BaseURL:  "https://openrouter.ai/api/v1",
	}, ai.Context{}, ai.StreamOptions{APIKey: "token", SessionID: "session-1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"session_id", "x-client-request-id", "x-session-affinity"} {
		if _, ok := defaultConfig.Headers[key]; ok {
			t.Fatalf("default completions compat should not send %s, got %#v", key, defaultConfig.Headers)
		}
	}

	enabledConfig, err := buildOpenAIClientConfig(ai.Model{
		API:      ai.ApiOpenAICompletions,
		Provider: ai.ProviderOpenRouter,
		BaseURL:  "https://openrouter.ai/api/v1",
		Compat:   map[string]any{"sendSessionAffinityHeaders": true},
	}, ai.Context{}, ai.StreamOptions{APIKey: "token", SessionID: "session-1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"session_id", "x-client-request-id", "x-session-affinity"} {
		if enabledConfig.Headers[key] != "session-1" {
			t.Fatalf("expected %s=session-1, got %#v", key, enabledConfig.Headers)
		}
	}
}

func TestOpenAIClientConfigAddsGitHubCopilotDynamicHeaders(t *testing.T) {
	config, err := buildOpenAIClientConfig(ai.Model{
		API:      ai.ApiOpenAIResponses,
		Provider: ai.ProviderGitHubCopilot,
	}, ai.Context{Messages: []ai.Message{{
		Role:    "toolResult",
		Content: []ai.ContentBlock{{Type: "image", MimeType: "image/png", Data: "abc"}},
	}}}, ai.StreamOptions{APIKey: "token"})
	if err != nil {
		t.Fatal(err)
	}
	if config.Headers["X-Initiator"] != "agent" || config.Headers["Openai-Intent"] != "conversation-edits" || config.Headers["Copilot-Vision-Request"] != "true" {
		t.Fatalf("expected Copilot dynamic headers, got %#v", config.Headers)
	}
}

func TestOpenAIClientConfigLetsHeadersOverrideCopilotDynamicHeaders(t *testing.T) {
	config, err := buildOpenAIClientConfig(ai.Model{
		API:      ai.ApiOpenAIResponses,
		Provider: ai.ProviderGitHubCopilot,
		Headers:  map[string]string{"X-Initiator": "model"},
	}, ai.Context{}, ai.StreamOptions{APIKey: "token", Headers: map[string]string{"X-Initiator": "caller"}})
	if err != nil {
		t.Fatal(err)
	}
	if config.Headers["X-Initiator"] != "caller" {
		t.Fatalf("expected explicit initiator override, got %#v", config.Headers)
	}
}

func TestOpenAIClientConfigCopilotDynamicHeadersOverrideModelDefaults(t *testing.T) {
	config, err := buildOpenAIClientConfig(ai.Model{
		API:      ai.ApiOpenAIResponses,
		Provider: ai.ProviderGitHubCopilot,
		Headers:  map[string]string{"X-Initiator": "model"},
	}, ai.Context{}, ai.StreamOptions{APIKey: "token"})
	if err != nil {
		t.Fatal(err)
	}
	if config.Headers["X-Initiator"] != "user" {
		t.Fatalf("expected Copilot dynamic header to override model default, got %#v", config.Headers)
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

func TestBuildCompletionsParamsSupportsA8CReasoningCompat(t *testing.T) {
	off := "off"
	model := ai.Model{
		ID:        "openai/gpt-oss-120b",
		API:       ai.ApiOpenAICompletions,
		Provider:  "a8c",
		BaseURL:   "https://ai-services.example/proxy/a8c/v1",
		Reasoning: true,
		Input:     []string{"text"},
		ThinkingLevelMap: map[ai.ModelThinkingLevel]*string{
			ai.ModelThinkingLevelOff: &off,
		},
	}
	offParams := BuildCompletionsParams(model, ai.Context{}, OpenAICompletionsOptions{})
	if offParams["include_reasoning"] != false {
		t.Fatalf("expected a8c off reasoning to use include_reasoning=false, got %#v", offParams)
	}
	if _, ok := offParams["reasoning_effort"]; ok {
		t.Fatalf("a8c off reasoning should not send rejected reasoning_effort=off, got %#v", offParams["reasoning_effort"])
	}

	minimal := ai.ThinkingLevelMinimal
	minimalParams := BuildCompletionsParams(model, ai.Context{}, OpenAICompletionsOptions{ReasoningEffort: &minimal})
	if minimalParams["include_reasoning"] != true {
		t.Fatalf("expected a8c enabled reasoning to include reasoning, got %#v", minimalParams)
	}
	if minimalParams["reasoning_effort"] != "low" {
		t.Fatalf("expected a8c minimal to map to low, got %#v", minimalParams["reasoning_effort"])
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
