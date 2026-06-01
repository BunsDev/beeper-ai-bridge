package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func TestCompletionsStreamStateKeepsContentIndexesSeparate(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream()
	output := newAssistant(testStreamModel())
	state := newCompletionsStreamState()

	state.apply(stream, &output, testStreamModel(), map[string]any{
		"id": "chatcmpl_1",
		"choices": []any{map[string]any{"delta": map[string]any{
			"content": "hello",
		}}},
	})
	state.apply(stream, &output, testStreamModel(), map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{
			"tool_calls": []any{map[string]any{"index": float64(0), "id": "call_1", "function": map[string]any{"name": "read", "arguments": `{"path"`}}},
		}}},
	})
	state.apply(stream, &output, testStreamModel(), map[string]any{
		"choices": []any{map[string]any{"finish_reason": "tool_calls", "delta": map[string]any{
			"tool_calls": []any{map[string]any{"index": float64(0), "function": map[string]any{"arguments": `:"x"}`}}},
		}}},
	})

	if !state.hasFinishReason {
		t.Fatal("expected finish reason")
	}
	if len(state.blocks) != 2 {
		t.Fatalf("expected text and tool blocks, got %#v", state.blocks)
	}
	if state.blocks[0].Type != "text" || state.blocks[0].Text != "hello" {
		t.Fatalf("expected text block first, got %#v", state.blocks[0])
	}
	if state.blocks[1].Type != "toolCall" || state.blocks[1].ID != "call_1" || state.blocks[1].Name != "read" {
		t.Fatalf("expected tool block second, got %#v", state.blocks[1])
	}
	if state.blocks[1].Arguments["path"] != "x" {
		t.Fatalf("expected parsed tool args, got %#v", state.blocks[1].Arguments)
	}
	if output.StopReason != ai.StopReasonToolUse {
		t.Fatalf("expected toolUse stop reason, got %q", output.StopReason)
	}
}

func TestCompletionsStreamStateParsesUsageAndReasoning(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream()
	model := testStreamModel()
	model.Cost = ai.ModelCost{Input: 1, Output: 2, CacheRead: 0.5, CacheWrite: 3}
	output := newAssistant(model)
	state := newCompletionsStreamState()

	state.apply(stream, &output, model, map[string]any{
		"usage": map[string]any{
			"prompt_tokens":     float64(10),
			"completion_tokens": float64(5),
			"prompt_tokens_details": map[string]any{
				"cached_tokens":      float64(3),
				"cache_write_tokens": float64(2),
			},
		},
		"choices": []any{map[string]any{"finish_reason": "stop", "delta": map[string]any{
			"reasoning_content": "thinking",
		}}},
	})
	if output.Usage.Input != 5 || output.Usage.Output != 5 || output.Usage.CacheRead != 3 || output.Usage.CacheWrite != 2 {
		t.Fatalf("unexpected usage: %#v", output.Usage)
	}
	if len(state.blocks) != 1 || state.blocks[0].Type != "thinking" || state.blocks[0].ThinkingSignature != "reasoning_content" {
		t.Fatalf("expected reasoning block, got %#v", state.blocks)
	}
}

func TestResponsesStreamStateFinalizesReasoningTextToolAndUsage(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream()
	model := testStreamModel()
	model.API = ai.ApiOpenAIResponses
	model.Cost = ai.ModelCost{Input: 1, Output: 2, CacheRead: 0.5}
	output := newAssistant(model)
	state := newResponsesStreamState()

	state.apply(stream, &output, model, OpenAIResponsesOptions{ServiceTier: "priority"}, map[string]any{
		"type": "response.output_item.added",
		"item": map[string]any{"type": "reasoning", "id": "rs_1"},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{"type": "response.reasoning_text.delta", "delta": "think"})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.done",
		"item": map[string]any{"type": "reasoning", "id": "rs_1", "content": []any{map[string]any{"text": "final think"}}},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.added",
		"item": map[string]any{"type": "message", "id": "msg_1"},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.content_part.added",
		"part": map[string]any{"type": "output_text"},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{"type": "response.output_text.delta", "delta": "hello"})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.done",
		"item": map[string]any{"type": "message", "id": "msg_1", "content": []any{map[string]any{"type": "output_text", "text": "hello"}}},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.added",
		"item": map[string]any{"type": "function_call", "id": "fc_1", "call_id": "call_1", "name": "read", "arguments": ""},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{"type": "response.function_call_arguments.delta", "delta": `{"path":"x"`})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{"type": "response.function_call_arguments.done", "arguments": `{"path":"x"}`})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.done",
		"item": map[string]any{"type": "function_call", "id": "fc_1", "call_id": "call_1", "name": "read", "arguments": `{"path":"x"}`},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{ServiceTier: "priority"}, map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id":           "resp_1",
			"status":       "completed",
			"service_tier": "flex",
			"usage": map[string]any{
				"input_tokens":  float64(10),
				"output_tokens": float64(5),
				"total_tokens":  float64(15),
				"input_tokens_details": map[string]any{
					"cached_tokens": float64(3),
				},
				"output_tokens_details": map[string]any{
					"reasoning_tokens": float64(4),
				},
			},
		},
	})

	if len(state.blocks) != 3 {
		t.Fatalf("expected three blocks, got %#v", state.blocks)
	}
	if state.blocks[0].Thinking != "final think" || state.blocks[0].ThinkingSignature == "" {
		t.Fatalf("expected finalized reasoning block, got %#v", state.blocks[0])
	}
	if state.blocks[1].Text != "hello" || state.blocks[1].TextSignature == "" {
		t.Fatalf("expected finalized text block, got %#v", state.blocks[1])
	}
	if state.blocks[2].Arguments["path"] != "x" {
		t.Fatalf("expected finalized tool args, got %#v", state.blocks[2])
	}
	if output.StopReason != ai.StopReasonToolUse {
		t.Fatalf("expected toolUse because response has tool call, got %q", output.StopReason)
	}
	if output.Usage.Input != 7 || output.Usage.CacheRead != 3 || output.Usage.Output != 5 || output.Usage.ReasoningTokens != 4 {
		t.Fatalf("unexpected usage: %#v", output.Usage)
	}
	if output.Usage.Cost.Total != 0.00000925 {
		t.Fatalf("expected response service-tier adjusted cost, got %#v", output.Usage.Cost)
	}
	if state.blocks[1].TextSignature != `{"id":"msg_1","v":1}` && state.blocks[1].TextSignature != `{"v":1,"id":"msg_1"}` {
		t.Fatalf("expected text signature without null phase, got %q", state.blocks[1].TextSignature)
	}
}

func TestResponsesStreamStateBackfillsTextAndReasoningDoneEvents(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream()
	model := testStreamModel()
	model.API = ai.ApiOpenAIResponses
	output := newAssistant(model)
	state := newResponsesStreamState()

	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.added",
		"item": map[string]any{"type": "reasoning", "id": "rs_1"},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type":    "response.reasoning_text.done",
		"item_id": "rs_1",
		"text":    "full reasoning",
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.added",
		"item": map[string]any{"type": "message", "id": "msg_1"},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type":    "response.output_text.done",
		"item_id": "msg_1",
		"text":    "full answer",
	})

	if state.blocks[0].Thinking != "full reasoning" || state.blocks[1].Text != "full answer" {
		t.Fatalf("expected done events to backfill content, got %#v", state.blocks)
	}
	events := drainAssistantEvents(stream)
	var thinkingDelta, textDelta bool
	for _, event := range events {
		if event.Type == "thinking_delta" && event.Delta == "full reasoning" {
			thinkingDelta = true
		}
		if event.Type == "text_delta" && event.Delta == "full answer" {
			textDelta = true
		}
	}
	if !thinkingDelta || !textDelta {
		t.Fatalf("expected done events to emit missing deltas, got %#v", events)
	}
}

func TestResponsesStreamStateBackfillsPartDoneEvents(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream()
	model := testStreamModel()
	model.API = ai.ApiOpenAIResponses
	output := newAssistant(model)
	state := newResponsesStreamState()

	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type":         "response.output_item.added",
		"output_index": 0,
		"item":         map[string]any{"type": "reasoning", "id": "rs_1"},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type":         "response.content_part.done",
		"output_index": 0,
		"part":         map[string]any{"type": "reasoning_text", "text": "part reasoning"},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type":         "response.output_item.added",
		"output_index": 1,
		"item":         map[string]any{"type": "message", "id": "msg_1"},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type":         "response.content_part.done",
		"output_index": 1,
		"part":         map[string]any{"type": "output_text", "text": "part answer"},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type":         "response.output_item.added",
		"output_index": 2,
		"item":         map[string]any{"type": "message", "id": "msg_2"},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type":         "response.refusal.done",
		"output_index": 2,
		"refusal":      "final refusal",
	})

	if state.blocks[0].Thinking != "part reasoning" || state.blocks[1].Text != "part answer" || state.blocks[2].Text != "final refusal" {
		t.Fatalf("expected part done events to backfill content, got %#v", state.blocks)
	}
	events := drainAssistantEvents(stream)
	var thinkingDelta, answerDelta, refusalDelta bool
	for _, event := range events {
		if event.Type == "thinking_delta" && event.Delta == "part reasoning" {
			thinkingDelta = true
		}
		if event.Type == "text_delta" && event.Delta == "part answer" {
			answerDelta = true
		}
		if event.Type == "text_delta" && event.Delta == "final refusal" {
			refusalDelta = true
		}
	}
	if !thinkingDelta || !answerDelta || !refusalDelta {
		t.Fatalf("expected part done events to emit missing deltas, got %#v", events)
	}
}

func TestResponsesStreamStateBackfillsReasoningSummaryPartDone(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream()
	model := testStreamModel()
	model.API = ai.ApiOpenAIResponses
	output := newAssistant(model)
	state := newResponsesStreamState()

	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.added",
		"item": map[string]any{"type": "reasoning", "id": "rs_1"},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type":    "response.reasoning_summary_part.added",
		"item_id": "rs_1",
		"part":    map[string]any{"type": "summary_text", "text": ""},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type":    "response.reasoning_summary_part.done",
		"item_id": "rs_1",
		"part":    map[string]any{"type": "summary_text", "text": "summary only on part done"},
	})

	if state.blocks[0].Thinking != "summary only on part done\n\n" {
		t.Fatalf("expected summary part done to backfill summary text, got %#v", state.blocks[0])
	}
	events := drainAssistantEvents(stream)
	var summaryDelta, separatorDelta bool
	for _, event := range events {
		if event.Type == "thinking_delta" && event.Delta == "summary only on part done" {
			summaryDelta = true
		}
		if event.Type == "thinking_delta" && event.Delta == "\n\n" {
			separatorDelta = true
		}
	}
	if !summaryDelta || !separatorDelta {
		t.Fatalf("expected summary part done to emit content and separator deltas, got %#v", events)
	}
}

func TestResponsesStreamStateMapsNativeWebSearchCallToToolActivity(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream()
	model := testStreamModel()
	model.API = ai.ApiOpenAIResponses
	output := newAssistant(model)
	state := newResponsesStreamState()

	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.added",
		"item": map[string]any{
			"type":   "web_search_call",
			"id":     "ws_1",
			"status": "in_progress",
			"action": map[string]any{"type": "search", "query": "latest headlines Amsterdam news today"},
		},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.done",
		"item": map[string]any{
			"type":   "web_search_call",
			"id":     "ws_1",
			"status": "completed",
			"action": map[string]any{"type": "search", "query": "latest headlines Amsterdam news today"},
		},
	})

	events := drainAssistantEvents(stream)
	if len(output.Content.([]ai.ContentBlock)) != 0 {
		t.Fatalf("native web search must not become an executable tool block, got %#v", output.Content)
	}
	var started, result bool
	for _, event := range events {
		switch event.Type {
		case "toolcall_start":
			started = event.ToolCall != nil && event.ToolCall.ID == "ws_1" && event.ToolCall.Name == "web_search"
		case "toolresult":
			result = event.ToolCall != nil && event.ToolCall.ID == "ws_1"
			if output, _ := event.CustomValue.(map[string]any); output["status"] != "success" || output["native"] != true {
				t.Fatalf("unexpected native search result payload %#v", event.CustomValue)
			}
		}
	}
	if !started || !result {
		t.Fatalf("expected native web search start/result events, got %#v", events)
	}
}

func TestResponsesStreamStateKeepsReasoningStreamingAcrossNativeToolItems(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream()
	model := testStreamModel()
	model.API = ai.ApiOpenAIResponses
	output := newAssistant(model)
	state := newResponsesStreamState()

	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.added",
		"item": map[string]any{"type": "reasoning", "id": "rs_1"},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type":    "response.reasoning_summary_part.added",
		"item_id": "rs_1",
		"part":    map[string]any{"type": "summary_text", "text": ""},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.added",
		"item": map[string]any{
			"type":   "web_search_call",
			"id":     "ws_1",
			"status": "searching",
			"action": map[string]any{"type": "search", "query": "Amsterdam news"},
		},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type":    "response.reasoning_summary_text.delta",
		"item_id": "rs_1",
		"delta":   "checking sources",
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.done",
		"item": map[string]any{"type": "web_search_call", "id": "ws_1", "status": "completed", "action": map[string]any{"type": "search", "query": "Amsterdam news"}},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.done",
		"item": map[string]any{"type": "reasoning", "id": "rs_1", "summary": []any{map[string]any{"type": "summary_text", "text": "checking sources"}}},
	})

	if len(state.blocks) != 1 || state.blocks[0].Thinking != "checking sources" {
		t.Fatalf("expected reasoning summary to survive native tool interleave, got %#v", state.blocks)
	}
	events := drainAssistantEvents(stream)
	var thinkingDelta, thinkingEnd bool
	for _, event := range events {
		if event.Type == "thinking_delta" && event.Delta == "checking sources" {
			thinkingDelta = true
		}
		if event.Type == "thinking_end" && event.Content == "checking sources" {
			thinkingEnd = true
		}
	}
	if !thinkingDelta || !thinkingEnd {
		t.Fatalf("expected streamed reasoning delta and end, got %#v", events)
	}
}

func TestResponsesStreamStateMapsOpenRouterWebSearchToToolActivity(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream()
	model := testStreamModel()
	model.API = ai.ApiOpenAIResponses
	model.Provider = ai.ProviderOpenRouter
	output := newAssistant(model)
	state := newResponsesStreamState()

	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.added",
		"item": map[string]any{
			"type":   "openrouter:web_search",
			"id":     "or_search_1",
			"status": "in_progress",
		},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.done",
		"item": map[string]any{
			"type":   "openrouter:web_search",
			"id":     "or_search_1",
			"status": "completed",
			"action": map[string]any{"type": "search", "query": "current Amsterdam headline"},
		},
	})

	events := drainAssistantEvents(stream)
	if len(output.Content.([]ai.ContentBlock)) != 0 {
		t.Fatalf("OpenRouter native search must not become an executable tool block, got %#v", output.Content)
	}
	var started, result bool
	for _, event := range events {
		switch event.Type {
		case "toolcall_start":
			started = event.ToolCall != nil && event.ToolCall.ID == "or_search_1" && event.ToolCall.Name == "web_search"
		case "toolresult":
			result = event.ToolCall != nil && event.ToolCall.ID == "or_search_1" && event.ToolCall.Arguments["query"] == "current Amsterdam headline"
			output, _ := event.CustomValue.(map[string]any)
			if output["provider"] != string(ai.ProviderOpenRouter) || output["status"] != "success" || output["query"] != "current Amsterdam headline" {
				t.Fatalf("unexpected OpenRouter search result payload %#v", event.CustomValue)
			}
		}
	}
	if !started || !result {
		t.Fatalf("expected OpenRouter web search start/result events, got %#v", events)
	}
}

func TestResponsesStreamStateMapsOpenRouterWebFetchToToolActivity(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream()
	model := testStreamModel()
	model.API = ai.ApiOpenAIResponses
	model.Provider = ai.ProviderOpenRouter
	output := newAssistant(model)
	state := newResponsesStreamState()

	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.added",
		"item": map[string]any{
			"type":   "openrouter:web_fetch",
			"id":     "or_fetch_1",
			"status": "in_progress",
		},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.done",
		"item": map[string]any{
			"type":       "openrouter:web_fetch",
			"id":         "or_fetch_1",
			"status":     "completed",
			"url":        "https://example.com/article",
			"title":      "Fetched Article",
			"httpStatus": float64(200),
			"content":    "Fetched Article body that should not be copied into the tool output.",
		},
	})

	events := drainAssistantEvents(stream)
	var sawSource, sawResult bool
	for _, event := range events {
		switch event.Type {
		case "source":
			sawSource = len(output.Citations) == 1 && output.Citations[0].URL == "https://example.com/article"
		case "toolresult":
			sawResult = event.ToolCall != nil && event.ToolCall.Name == "fetch"
			output, _ := event.CustomValue.(map[string]any)
			if output["provider"] != string(ai.ProviderOpenRouter) || output["url"] != "https://example.com/article" || output["title"] != "Fetched Article" || output["content"] != nil {
				t.Fatalf("unexpected OpenRouter fetch result payload %#v", event.CustomValue)
			}
		}
	}
	if !sawSource || !sawResult {
		t.Fatalf("expected OpenRouter fetch source/result events, got %#v citations=%#v", events, output.Citations)
	}
}

func TestResponsesStreamStateMapsOpenRouterWebFetchFailure(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream()
	model := testStreamModel()
	model.API = ai.ApiOpenAIResponses
	model.Provider = ai.ProviderOpenRouter
	output := newAssistant(model)
	state := newResponsesStreamState()

	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.done",
		"item": map[string]any{
			"type":   "openrouter:web_fetch",
			"id":     "or_fetch_fail",
			"status": "incomplete",
			"url":    "https://nonexistent.invalid/does-not-exist",
			"error":  "Exa returned no content for this URL.",
		},
	})

	events := drainAssistantEvents(stream)
	var result bool
	for _, event := range events {
		if event.Type != "toolresult" {
			continue
		}
		result = event.ToolCall != nil && event.ToolCall.Name == "fetch"
		output, _ := event.CustomValue.(map[string]any)
		if output["status"] != "failed" || output["reason"] != "Exa returned no content for this URL." {
			t.Fatalf("unexpected OpenRouter fetch failure payload %#v", event.CustomValue)
		}
	}
	if !result {
		t.Fatalf("expected OpenRouter fetch failure result, got %#v", events)
	}
}

func TestResponsesStreamStateStreamsTextDeltasWithoutContentPartPrelude(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream()
	model := testStreamModel()
	model.API = ai.ApiOpenAIResponses
	output := newAssistant(model)
	state := newResponsesStreamState()

	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.added",
		"item": map[string]any{"type": "message", "id": "msg_1"},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{"type": "response.output_text.delta", "delta": "direct"})
	if state.blocks[0].Text != "direct" {
		t.Fatalf("expected text delta without content part prelude to stream, got %#v", state.blocks[0])
	}
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.done",
		"item": map[string]any{"type": "message", "id": "msg_1", "content": []any{map[string]any{"type": "output_text", "text": "direct"}}},
	})

	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.added",
		"item": map[string]any{"type": "message", "id": "msg_2"},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.content_part.added",
		"part": map[string]any{"type": "refusal"},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{"type": "response.output_text.delta", "delta": "still ignored"})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{"type": "response.refusal.delta", "delta": "refused"})
	if state.blocks[1].Text != "refused" {
		t.Fatalf("expected only matching refusal delta, got %#v", state.blocks[1])
	}

	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.added",
		"item": map[string]any{"type": "reasoning", "id": "rs_1"},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{"type": "response.reasoning_summary_text.delta", "delta": "ignored"})
	if state.blocks[2].Thinking != "" {
		t.Fatalf("expected summary delta before summary part to be ignored, got %#v", state.blocks[2])
	}
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.reasoning_summary_part.added",
		"part": map[string]any{"type": "summary_text", "text": ""},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{"type": "response.reasoning_summary_text.delta", "delta": "summary"})
	if state.blocks[2].Thinking != "summary" {
		t.Fatalf("expected summary delta after summary part, got %#v", state.blocks[2])
	}
}

func drainAssistantEvents(stream *ai.AssistantMessageEventStream) []ai.AssistantMessageEvent {
	events := []ai.AssistantMessageEvent{}
	for {
		select {
		case event := <-stream.Events():
			events = append(events, event)
		default:
			return events
		}
	}
}

func TestResponsesStreamStateFinalizesImageGenerationCall(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream()
	model := testStreamModel()
	model.API = ai.ApiOpenAIResponses
	output := newAssistant(model)
	state := newResponsesStreamState()

	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.added",
		"item": map[string]any{"type": "image_generation_call", "id": "ig_1"},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.done",
		"item": map[string]any{
			"type":   "image_generation_call",
			"id":     "ig_1",
			"status": "completed",
			"result": "data:image/webp;base64,abc123",
		},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type":     "response.completed",
		"response": map[string]any{"status": "completed"},
	})

	if len(state.blocks) != 1 {
		t.Fatalf("expected image block, got %#v", state.blocks)
	}
	block := state.blocks[0]
	if block.Type != "image" || block.ID != "ig_1" || block.MimeType != "image/webp" || block.Data != "abc123" {
		t.Fatalf("unexpected image block: %#v", block)
	}
	if output.StopReason != ai.StopReasonStop {
		t.Fatalf("expected stop reason, got %q", output.StopReason)
	}
}

func TestResponsesStreamStateFinalizesMultipleImageGenerationCalls(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream()
	model := testStreamModel()
	model.API = ai.ApiOpenAIResponses
	output := newAssistant(model)
	state := newResponsesStreamState()

	for _, item := range []map[string]any{
		{"type": "image_generation_call", "id": "ig_1", "result": "first"},
		{"type": "image_generation_call", "id": "ig_2", "result": "second"},
	} {
		state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
			"type": "response.output_item.added",
			"item": map[string]any{"type": item["type"], "id": item["id"]},
		})
		state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
			"type": "response.output_item.done",
			"item": item,
		})
	}

	if len(state.blocks) != 2 {
		t.Fatalf("expected two image blocks, got %#v", state.blocks)
	}
	if state.blocks[0].ID != "ig_1" || state.blocks[0].Data != "first" || state.blocks[1].ID != "ig_2" || state.blocks[1].Data != "second" {
		t.Fatalf("unexpected image blocks: %#v", state.blocks)
	}
}

func TestResponsesStreamStateSeedsFunctionCallArgumentsFromAddedItem(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream()
	model := testStreamModel()
	model.API = ai.ApiOpenAIResponses
	output := newAssistant(model)
	state := newResponsesStreamState()

	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.output_item.added",
		"item": map[string]any{"type": "function_call", "id": "fc_1", "call_id": "call_1", "name": "read", "arguments": `{"path"`},
	})
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type":  "response.function_call_arguments.delta",
		"delta": `:"x"}`,
	})

	if len(state.blocks) != 1 || state.blocks[0].Arguments["path"] != "x" {
		t.Fatalf("expected initial item arguments and delta to combine, got %#v", state.blocks)
	}
}

func TestFinishResponsesStreamEmitsErrorForFailedResponse(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream()
	model := testStreamModel()
	model.API = ai.ApiOpenAIResponses
	output := newAssistant(model)
	state := newResponsesStreamState()
	state.apply(stream, &output, model, OpenAIResponsesOptions{}, map[string]any{
		"type": "response.failed",
		"response": map[string]any{
			"error": map[string]any{"code": "bad_request", "message": "nope"},
		},
	})

	finishResponsesStream(stream, &output, state)
	event := <-stream.Events()
	for event.Type != "error" {
		event = <-stream.Events()
	}
	if event.Error == nil || event.Error.StopReason != ai.StopReasonError || event.Error.ErrorMessage != "bad_request: nope" {
		t.Fatalf("expected failed response error event, got %#v", event)
	}
}

func TestOpenAIResponsesStreamDoesNotSleepOnHugeRetryAfterByDefault(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "32976")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"AI token limit exceeded","type":"rate_limit_exceeded","code":"rate_limit_exceeded"}}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	started := time.Now()
	result := StreamOpenAIResponses(ctx, ai.Model{
		ID:       "gpt-test",
		API:      ai.ApiOpenAIResponses,
		Provider: ai.ProviderOpenAI,
		BaseURL:  server.URL,
		Input:    []string{"text"},
	}, ai.Context{Messages: []ai.Message{{Role: "user", Content: "hi"}}}, OpenAIResponsesOptions{
		StreamOptions: ai.StreamOptions{APIKey: "key"},
	}).Result()
	elapsed := time.Since(started)

	if elapsed > 500*time.Millisecond {
		t.Fatalf("stream waited on Retry-After before returning: %s", elapsed)
	}
	if attempts != 1 {
		t.Fatalf("expected no implicit retry attempts, got %d", attempts)
	}
	if result.StopReason != ai.StopReasonError || !strings.Contains(result.ErrorMessage, "429") {
		t.Fatalf("expected terminal 429 error result, got %#v", result)
	}
}

func testStreamModel() ai.Model {
	return ai.Model{ID: "gpt-test", API: ai.ApiOpenAICompletions, Provider: "openai", Input: []string{"text"}}
}
