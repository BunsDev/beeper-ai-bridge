package msgconv

import (
	"testing"

	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func TestStreamDeltaDoneIncludesFinalTextUsageAndResponse(t *testing.T) {
	delta := StreamDelta(ai.AssistantMessageEvent{
		Type:   "done",
		Reason: ai.StopReasonStop,
		Message: &ai.Message{
			Content:    []ai.ContentBlock{{Type: "text", Text: "hello"}},
			Usage:      ai.Usage{Input: 1, Output: 2},
			ResponseID: "resp_123",
		},
	})
	if delta["op"] != "done" || delta["text"] != "hello" || delta["response_id"] != "resp_123" {
		t.Fatalf("unexpected done delta %#v", delta)
	}
	usage, ok := delta["usage"].(ai.Usage)
	if !ok || usage.Input != 1 || usage.Output != 2 {
		t.Fatalf("unexpected usage %#v", delta["usage"])
	}
}

func TestStreamDeltaToolCallStart(t *testing.T) {
	call := &ai.ToolCall{Type: "function", ID: "call_1", Name: "read_file"}
	delta := StreamDelta(ai.AssistantMessageEvent{Type: "tool_call", ToolCall: call})
	if delta["op"] != "tool_call_start" || delta["tool_call"] != call {
		t.Fatalf("unexpected tool delta %#v", delta)
	}
}

func TestStreamDeltaToolCallLifecycle(t *testing.T) {
	start := StreamDelta(ai.AssistantMessageEvent{Type: "toolcall_start", ContentIndex: 1})
	if start["op"] != "tool_call_start" || start["content_index"] != 1 {
		t.Fatalf("unexpected start delta %#v", start)
	}
	argDelta := StreamDelta(ai.AssistantMessageEvent{Type: "toolcall_delta", ContentIndex: 1, Delta: `{"path"`})
	if argDelta["op"] != "tool_call_delta" || argDelta["delta"] != `{"path"` {
		t.Fatalf("unexpected arg delta %#v", argDelta)
	}
	call := &ai.ToolCall{Type: "toolCall", ID: "call_1", Name: "read_file"}
	done := StreamDelta(ai.AssistantMessageEvent{Type: "toolcall_end", ContentIndex: 1, ToolCall: call})
	if done["op"] != "tool_call_done" || done["tool_call"] != call {
		t.Fatalf("unexpected done delta %#v", done)
	}
}
