package providers

import (
	"strings"
	"testing"

	ai "github.com/beeper/ai-bridge/pkg/ai"
)

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
