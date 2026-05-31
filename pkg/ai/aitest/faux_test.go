package aitest

import (
	"context"
	"testing"

	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func TestFauxProviderStreamsThinkingTextAndToolCalls(t *testing.T) {
	registration := RegisterFauxProvider(RegisterFauxProviderOptions{
		Models:    []FauxModelDefinition{{ID: "faux-test", Reasoning: true}},
		TokenSize: TokenSize{Min: 1, Max: 1},
	})
	defer registration.Unregister()
	registration.SetMessages(AssistantBlocks([]ai.ContentBlock{
		Thinking("go"),
		Text("ok"),
		ToolCall("echo", map[string]any{"text": "hi"}, "tool-1"),
	}, WithStopReason(ai.StopReasonToolUse)))

	stream := ai.StreamSimple(context.Background(), registration.MustModel("faux-test"), ai.Context{
		Messages: []ai.Message{{Role: "user", Content: "hi"}},
	}, ai.SimpleStreamOptions{})

	var eventTypes []string
	var toolArgs string
	for event := range stream.Events() {
		eventTypes = append(eventTypes, event.Type)
		if event.Type == "toolcall_delta" {
			toolArgs += event.Delta
		}
	}
	if got, want := eventTypes, []string{
		"start",
		"thinking_start",
		"thinking_delta",
		"thinking_end",
		"text_start",
		"text_delta",
		"text_end",
		"toolcall_start",
	}; !hasPrefix(got, want) {
		t.Fatalf("unexpected event prefix\ngot  %#v\nwant %#v", got, want)
	}
	if eventTypes[len(eventTypes)-2] != "toolcall_end" || eventTypes[len(eventTypes)-1] != "done" {
		t.Fatalf("unexpected terminal events %#v", eventTypes)
	}
	if toolArgs != `{"text":"hi"}` {
		t.Fatalf("unexpected tool args delta %q", toolArgs)
	}
	result := stream.Result()
	if result.StopReason != ai.StopReasonToolUse || result.Provider != registration.Provider || result.Model != "faux-test" {
		t.Fatalf("unexpected result %#v", result)
	}
}

func TestFauxProviderFactoriesAndPromptCache(t *testing.T) {
	registration := RegisterFauxProvider(RegisterFauxProviderOptions{
		Models: []FauxModelDefinition{
			{ID: "faux-fast", Reasoning: false},
			{ID: "faux-thinker", Reasoning: true},
		},
	})
	defer registration.Unregister()
	registration.SetResponses(
		Factory(func(ctx context.Context, llmContext ai.Context, options ai.SimpleStreamOptions, state ResponseState, model ai.Model) (ai.Message, error) {
			if len(llmContext.Messages) != 1 || llmContext.Messages[0].Role != "user" {
				t.Fatalf("unexpected context %#v", llmContext)
			}
			return AssistantText(model.ID), nil
		}),
		Factory(func(ctx context.Context, llmContext ai.Context, options ai.SimpleStreamOptions, state ResponseState, model ai.Model) (ai.Message, error) {
			return AssistantText(model.ID), nil
		}),
	)

	contextMessages := ai.Context{SystemPrompt: "sys", Messages: []ai.Message{{Role: "user", Content: "hello"}}}
	first := ai.CompleteSimple(context.Background(), registration.MustModel("faux-fast"), contextMessages, ai.SimpleStreamOptions{
		StreamOptions: ai.StreamOptions{SessionID: "session-1", CacheRetention: ai.CacheRetentionShort},
	})
	if textFromResult(first) != "faux-fast" || first.Usage.CacheRead != 0 || first.Usage.CacheWrite == 0 {
		t.Fatalf("unexpected first response %#v", first)
	}
	contextMessages.Messages = append(contextMessages.Messages, first, ai.Message{Role: "user", Content: "again"})
	second := ai.CompleteSimple(context.Background(), registration.MustModel("faux-thinker"), contextMessages, ai.SimpleStreamOptions{
		StreamOptions: ai.StreamOptions{SessionID: "session-1", CacheRetention: ai.CacheRetentionShort},
	})
	if textFromResult(second) != "faux-thinker" || second.Usage.CacheRead == 0 {
		t.Fatalf("unexpected second response %#v", second)
	}
	if registration.State.CallCount != 2 || registration.PendingResponseCount() != 0 {
		t.Fatalf("unexpected registration state %#v pending=%d", registration.State, registration.PendingResponseCount())
	}
}

func TestFauxProviderErrorsWhenResponsesAreExhausted(t *testing.T) {
	registration := RegisterFauxProvider(RegisterFauxProviderOptions{})
	defer registration.Unregister()
	result := ai.CompleteSimple(context.Background(), registration.MustModel(), ai.Context{}, ai.SimpleStreamOptions{})
	if result.StopReason != ai.StopReasonError || result.ErrorMessage != "No more faux responses queued" {
		t.Fatalf("unexpected exhausted result %#v", result)
	}
}

func textFromResult(message ai.Message) string {
	blocks, ok := message.Content.([]ai.ContentBlock)
	if !ok || len(blocks) == 0 {
		return ""
	}
	return blocks[0].Text
}

func hasPrefix(a []string, b []string) bool {
	if len(a) < len(b) {
		return false
	}
	for i := range a {
		if i == len(b) {
			return true
		}
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
