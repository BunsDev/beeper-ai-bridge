package providers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func TestFormatAnthropicAPIErrorParsesProviderBody(t *testing.T) {
	got := formatAnthropicAPIError(http.StatusBadRequest, []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"messages.0.content: Field required"}}`))
	want := "Anthropic API error (400): messages.0.content: Field required"
	if got != want {
		t.Fatalf("formatAnthropicAPIError() = %q, want %q", got, want)
	}
}

func TestFormatAnthropicAPIErrorPreservesEmptyBodyStatus(t *testing.T) {
	got := formatAnthropicAPIError(http.StatusBadRequest, nil)
	want := "Anthropic API error (400): 400 Bad Request (no body)"
	if got != want {
		t.Fatalf("formatAnthropicAPIError() = %q, want %q", got, want)
	}
}

func TestStreamAnthropicErrorsWhenStreamEndsBeforeMessageStop(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"))
	}))
	defer upstream.Close()

	model := ai.Model{ID: "claude-test", API: ai.ApiAnthropicMessages, Provider: ai.ProviderAnthropic, BaseURL: upstream.URL}
	result := StreamAnthropic(t.Context(), model, ai.Context{}, AnthropicOptions{StreamOptions: ai.StreamOptions{APIKey: "key"}}).Result()
	if result.StopReason != ai.StopReasonError || !strings.Contains(result.ErrorMessage, "message_stop") {
		t.Fatalf("expected missing message_stop error, got %#v", result)
	}
}

func TestAnthropicStreamStatePreservesToolInputFromContentBlockStart(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream()
	model := ai.Model{ID: "claude-test", API: ai.ApiAnthropicMessages, Provider: ai.ProviderAnthropic}
	output := newAssistant(model)
	state := newAnthropicStreamState()

	state.apply(stream, &output, model, ai.Context{}, false, map[string]any{
		"type":  "content_block_start",
		"index": float64(0),
		"content_block": map[string]any{
			"type":  "tool_use",
			"id":    "toolu_1",
			"name":  "read",
			"input": map[string]any{"path": "README.md"},
		},
	})
	state.apply(stream, &output, model, ai.Context{}, false, map[string]any{
		"type":  "content_block_stop",
		"index": float64(0),
	})

	blocks := output.Content.([]ai.ContentBlock)
	if len(blocks) != 1 || blocks[0].Type != "toolCall" {
		t.Fatalf("expected one tool call block, got %#v", blocks)
	}
	if blocks[0].Arguments["path"] != "README.md" {
		t.Fatalf("expected tool input from content block start, got %#v", blocks[0].Arguments)
	}
}

func TestAnthropicStreamStateMapsNativeWebSearchToToolActivity(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream()
	model := ai.Model{ID: "claude-test", API: ai.ApiAnthropicMessages, Provider: ai.ProviderAnthropic}
	output := newAssistant(model)
	state := newAnthropicStreamState()

	state.apply(stream, &output, model, ai.Context{}, false, map[string]any{
		"type":  "content_block_start",
		"index": float64(0),
		"content_block": map[string]any{
			"type": "server_tool_use",
			"id":   "srvtoolu_1",
			"name": "web_search",
		},
	})
	state.apply(stream, &output, model, ai.Context{}, false, map[string]any{
		"type":  "content_block_delta",
		"index": float64(0),
		"delta": map[string]any{"type": "input_json_delta", "partial_json": `{"query":"latest headlines Amsterdam news today"}`},
	})
	state.apply(stream, &output, model, ai.Context{}, false, map[string]any{
		"type":  "content_block_start",
		"index": float64(1),
		"content_block": map[string]any{
			"type":        "web_search_tool_result",
			"tool_use_id": "srvtoolu_1",
			"content": []any{map[string]any{
				"type":     "web_search_result",
				"url":      "https://example.com/news",
				"title":    "Amsterdam News",
				"page_age": "June 1, 2026",
			}},
		},
	})
	state.apply(stream, &output, model, ai.Context{}, false, map[string]any{
		"type":  "content_block_stop",
		"index": float64(1),
	})

	events := drainAssistantEvents(stream)
	if len(output.Content.([]ai.ContentBlock)) != 0 {
		t.Fatalf("native web search must not become an executable tool block, got %#v", output.Content)
	}
	var sawStart, sawArgs, sawResult bool
	for _, event := range events {
		switch event.Type {
		case "toolcall_start":
			sawStart = event.ToolCall != nil && event.ToolCall.ID == "srvtoolu_1" && event.ToolCall.Name == "web_search"
		case "toolcall_delta":
			sawArgs = event.ToolCall != nil && event.ToolCall.Arguments["query"] == "latest headlines Amsterdam news today"
		case "toolresult":
			sawResult = event.ToolCall != nil && event.ToolCall.ID == "srvtoolu_1"
			output, _ := event.CustomValue.(map[string]any)
			results, _ := output["results"].([]map[string]any)
			if output["status"] != "success" || output["native"] != true || len(results) != 1 || results[0]["title"] != "Amsterdam News" {
				t.Fatalf("unexpected native search result payload %#v", event.CustomValue)
			}
		}
	}
	if !sawStart || !sawArgs || !sawResult {
		t.Fatalf("expected native web search lifecycle events, got %#v", events)
	}
}

func TestAnthropicStreamStateMapsNativeWebFetchError(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream()
	model := ai.Model{ID: "claude-test", API: ai.ApiAnthropicMessages, Provider: ai.ProviderAnthropic}
	output := newAssistant(model)
	state := newAnthropicStreamState()

	state.apply(stream, &output, model, ai.Context{}, false, map[string]any{
		"type":  "content_block_start",
		"index": float64(0),
		"content_block": map[string]any{
			"type":  "server_tool_use",
			"id":    "srvtoolu_fetch",
			"name":  "web_fetch",
			"input": map[string]any{"url": "https://example.com/article"},
		},
	})
	state.apply(stream, &output, model, ai.Context{}, false, map[string]any{
		"type":  "content_block_start",
		"index": float64(1),
		"content_block": map[string]any{
			"type":        "web_fetch_tool_result",
			"tool_use_id": "srvtoolu_fetch",
			"content": map[string]any{
				"type":       "web_fetch_tool_error",
				"error_code": "url_not_accessible",
			},
		},
	})
	state.apply(stream, &output, model, ai.Context{}, false, map[string]any{
		"type":  "content_block_stop",
		"index": float64(1),
	})

	events := drainAssistantEvents(stream)
	var sawResult bool
	for _, event := range events {
		if event.Type != "toolresult" {
			continue
		}
		sawResult = event.ToolCall != nil && event.ToolCall.Name == "fetch"
		output, _ := event.CustomValue.(map[string]any)
		if output["status"] != "failed" || output["reason"] != "url_not_accessible" {
			t.Fatalf("unexpected native fetch error payload %#v", event.CustomValue)
		}
	}
	if !sawResult {
		t.Fatalf("expected native fetch error result, got %#v", events)
	}
}

func TestAnthropicStreamStateMapsNativeWebFetchDocumentTitle(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream()
	model := ai.Model{ID: "claude-test", API: ai.ApiAnthropicMessages, Provider: ai.ProviderAnthropic}
	output := newAssistant(model)
	state := newAnthropicStreamState()

	state.apply(stream, &output, model, ai.Context{}, false, map[string]any{
		"type":  "content_block_start",
		"index": float64(0),
		"content_block": map[string]any{
			"type":  "server_tool_use",
			"id":    "srvtoolu_fetch",
			"name":  "web_fetch",
			"input": map[string]any{"url": "https://example.com/article"},
		},
	})
	state.apply(stream, &output, model, ai.Context{}, false, map[string]any{
		"type":  "content_block_start",
		"index": float64(1),
		"content_block": map[string]any{
			"type":        "web_fetch_tool_result",
			"tool_use_id": "srvtoolu_fetch",
			"content": map[string]any{
				"type": "web_fetch_result",
				"url":  "https://example.com/article",
				"content": map[string]any{
					"type": "document",
					"source": map[string]any{
						"type":       "text",
						"media_type": "text/plain",
						"data":       "Fetched Article Title\n\nBody text",
					},
				},
			},
		},
	})

	events := drainAssistantEvents(stream)
	for _, event := range events {
		if event.Type != "toolresult" {
			continue
		}
		output, _ := event.CustomValue.(map[string]any)
		if output["title"] != "Fetched Article Title" {
			t.Fatalf("expected title from document source, got %#v", event.CustomValue)
		}
		return
	}
	t.Fatalf("expected native fetch result, got %#v", events)
}

func TestAnthropicBeeperProxyUsesBearerAuth(t *testing.T) {
	model := ai.Model{ID: "claude-test", API: ai.ApiAnthropicMessages, Provider: ai.ProviderAnthropic, BaseURL: "https://ai-services.beeper.localtest.me/proxy/anthropic"}
	headers := anthropicHeaders(model, ai.Context{}, AnthropicOptions{StreamOptions: ai.StreamOptions{APIKey: "matrix-token"}}, false)
	if headers["Authorization"] != "Bearer matrix-token" {
		t.Fatalf("expected bearer auth, got %#v", headers)
	}
	if _, ok := headers["X-Api-Key"]; ok {
		t.Fatalf("did not expect upstream Anthropic API key header for Beeper proxy: %#v", headers)
	}
}

func TestAnthropicBeeperProxyForwardsSessionAffinityWhenSupported(t *testing.T) {
	model := ai.Model{
		ID:       "claude-test",
		API:      ai.ApiAnthropicMessages,
		Provider: ai.ProviderAnthropic,
		BaseURL:  "https://ai-services.beeper.localtest.me/proxy/anthropic",
		Compat:   map[string]any{"sendSessionAffinityHeaders": true},
	}
	headers := anthropicHeaders(model, ai.Context{}, AnthropicOptions{StreamOptions: ai.StreamOptions{APIKey: "matrix-token", SessionID: "session-1"}}, false)
	if headers["X-Session-Affinity"] != "session-1" {
		t.Fatalf("expected forwarded session affinity, got %#v", headers)
	}

	disabled := anthropicHeaders(model, ai.Context{}, AnthropicOptions{StreamOptions: ai.StreamOptions{APIKey: "matrix-token", SessionID: "session-1", CacheRetention: ai.CacheRetentionNone}}, false)
	if _, ok := disabled["X-Session-Affinity"]; ok {
		t.Fatalf("cacheRetention=none should suppress session affinity, got %#v", disabled)
	}
}

func TestAnthropicHeadersIncludeWebFetchBetaFromMetadata(t *testing.T) {
	model := ai.Model{ID: "claude-test", API: ai.ApiAnthropicMessages, Provider: ai.ProviderAnthropic}
	headers := anthropicHeaders(model, ai.Context{}, AnthropicOptions{StreamOptions: ai.StreamOptions{
		APIKey:   "token",
		Metadata: map[string]any{webFetchBetaMetadataKey: true},
	}}, false)
	if !strings.Contains(headers["Anthropic-Beta"], webFetchBeta) || !strings.Contains(headers["Anthropic-Beta"], interleavedThinkingBeta) {
		t.Fatalf("expected web fetch beta to compose with generated betas, got %#v", headers)
	}
}

func TestAnthropicOpus47PlusUsesAdaptiveThinkingByDefault(t *testing.T) {
	model := ai.Model{
		ID:            "anthropic/claude-opus-4.8",
		API:           ai.ApiAnthropicMessages,
		Provider:      ai.ProviderAnthropic,
		Reasoning:     true,
		ReasoningMode: "adaptive",
		MaxTokens:     128000,
	}
	if !getAnthropicCompat(model).ForceAdaptiveThinking {
		t.Fatal("expected catalog adaptive reasoning mode to force adaptive thinking")
	}

	enabled := true
	params := BuildAnthropicParams(model, ai.Context{Messages: []ai.Message{{Role: "user", Content: "hi"}}}, false, AnthropicOptions{
		ThinkingEnabled: &enabled,
		Effort:          "high",
	})
	thinking, ok := params["thinking"].(map[string]any)
	if !ok || thinking["type"] != "adaptive" {
		t.Fatalf("expected adaptive thinking params, got %#v", params["thinking"])
	}
	if _, ok := thinking["budget_tokens"]; ok {
		t.Fatalf("adaptive thinking must not include manual budget_tokens: %#v", thinking)
	}
	outputConfig, ok := params["output_config"].(map[string]any)
	if !ok || outputConfig["effort"] != "high" {
		t.Fatalf("expected adaptive effort output_config, got %#v", params["output_config"])
	}
}
