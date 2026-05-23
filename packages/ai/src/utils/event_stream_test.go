package utils

import (
	"testing"

	ai "github.com/earendil-works/pi-mono/packages/ai/src"
)

func TestCreateAssistantMessageEventStreamFactory(t *testing.T) {
	stream := CreateAssistantMessageEventStream()
	message := ai.Message{Role: "assistant", StopReason: ai.StopReasonStop}
	stream.Push(ai.AssistantMessageEvent{Type: "done", Reason: ai.StopReasonStop, Message: &message})
	if result := stream.Result(); result.Role != "assistant" || result.StopReason != ai.StopReasonStop {
		t.Fatalf("unexpected result %#v", result)
	}
}
