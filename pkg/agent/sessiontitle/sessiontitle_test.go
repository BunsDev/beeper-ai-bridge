package sessiontitle

import (
	"context"
	"testing"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func TestGenerateCleansAndLimitsTitle(t *testing.T) {
	title, err := Generate(context.Background(), []agent.AgentMessage{{Role: "user", Content: "build a Matrix AI bridge"}}, Options{
		Model: ai.Model{ID: "title"},
		StreamFn: func(ctx context.Context, model ai.Model, llmContext ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			stream := ai.NewAssistantMessageEventStream()
			message := ai.Message{Role: "assistant", Content: []ai.ContentBlock{{Type: "text", Text: `"Matrix AI Bridge Implementation With Too Many Words."`}}, StopReason: ai.StopReasonStop}
			stream.Push(ai.AssistantMessageEvent{Type: "done", Message: &message})
			stream.End()
			return stream
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if title != "Matrix AI Bridge Implementation With Too Many" {
		t.Fatalf("unexpected title %q", title)
	}
}
