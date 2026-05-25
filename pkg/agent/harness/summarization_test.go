package harness

import (
	"context"
	"strings"
	"testing"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func TestGenerateSummaryUsesStreamFunction(t *testing.T) {
	ctx := context.Background()
	preparation := CompactionPreparation{
		FirstKeptEntryID:    "keep",
		MessagesToSummarize: []agent.AgentMessage{{Role: "user", Content: "hello", Timestamp: 1}},
		TokensBefore:        100,
		Settings:            CompactionSettings{ReserveTokens: 100, KeepRecentTokens: 10},
	}
	var captured ai.Context
	result, err := GenerateSummary(ctx, preparation, SummaryGenerationOptions{
		Model:    ai.Model{ID: "gpt-test", API: ai.ApiOpenAICompletions, Provider: "openai", MaxTokens: 50},
		APIKey:   "key",
		StreamFn: summaryTestStreamFn(t, "summary text", &captured),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FirstKeptEntryID != "keep" || result.TokensBefore != 100 {
		t.Fatalf("unexpected result %#v", result)
	}
	if !strings.Contains(result.Summary, "summary text") || strings.Contains(result.Summary, "<read-files>") {
		t.Fatalf("unexpected summary %q", result.Summary)
	}
	if captured.SystemPrompt != SummarizationSystemPrompt {
		t.Fatalf("unexpected system prompt")
	}
	if !strings.Contains(textFromContent(captured.Messages[0].Content), "<conversation>\n[User]: hello\n</conversation>") {
		t.Fatalf("unexpected prompt %#v", captured.Messages[0].Content)
	}
}

func TestGenerateBranchSummaryAddsPreambleAndUsesPrompt(t *testing.T) {
	ctx := context.Background()
	preparation := BranchPreparation{Messages: []agent.AgentMessage{{Role: "user", Content: "branch", Timestamp: 1}}}
	var captured ai.Context
	result, err := GenerateBranchSummary(ctx, preparation, SummaryGenerationOptions{
		Model:    ai.Model{ID: "gpt-test", API: ai.ApiOpenAICompletions, Provider: "openai"},
		StreamFn: summaryTestStreamFn(t, "branch text", &captured),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.Summary, branchSummaryPreamble) || !strings.Contains(result.Summary, "branch text") {
		t.Fatalf("unexpected branch summary %q", result.Summary)
	}
	if !strings.Contains(textFromContent(captured.Messages[0].Content), "Create a structured summary of this conversation branch") {
		t.Fatalf("unexpected branch prompt %#v", captured.Messages[0].Content)
	}
}

func TestGenerateBranchSummaryCanReplaceDefaultInstructions(t *testing.T) {
	ctx := context.Background()
	preparation := BranchPreparation{Messages: []agent.AgentMessage{{Role: "user", Content: "branch", Timestamp: 1}}}
	var captured ai.Context
	_, err := GenerateBranchSummary(ctx, preparation, SummaryGenerationOptions{
		Model:               ai.Model{ID: "gpt-test", API: ai.ApiOpenAICompletions, Provider: "openai"},
		StreamFn:            summaryTestStreamFn(t, "branch text", &captured),
		CustomInstructions:  "Only list changed files.",
		ReplaceInstructions: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	prompt := textFromContent(captured.Messages[0].Content)
	if strings.Contains(prompt, "Create a structured summary of this conversation branch") {
		t.Fatalf("expected default instructions to be replaced, got %q", prompt)
	}
	if !strings.Contains(prompt, "Only list changed files.") {
		t.Fatalf("expected custom instructions, got %q", prompt)
	}
}

func summaryTestStreamFn(t *testing.T, text string, captured *ai.Context) agent.StreamFn {
	t.Helper()
	return func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		*captured = llmContext
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			output := ai.Message{Role: "assistant", Content: []ai.ContentBlock{{Type: "text", Text: text}}, API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopReasonStop}
			stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: ai.StopReasonStop, Message: &output})
		}()
		return stream
	}
}
