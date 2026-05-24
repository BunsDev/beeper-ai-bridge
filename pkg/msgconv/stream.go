package msgconv

import (
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func StreamDelta(event ai.AssistantMessageEvent) map[string]any {
	switch event.Type {
	case "text_delta":
		return map[string]any{"op": "text_delta", "content_index": event.ContentIndex, "delta": event.Delta}
	case "toolcall_start", "tool_call":
		if event.ToolCall == nil {
			return map[string]any{"op": "tool_call_start", "content_index": event.ContentIndex}
		}
		return map[string]any{"op": "tool_call_start", "content_index": event.ContentIndex, "tool_call": event.ToolCall}
	case "toolcall_delta":
		return map[string]any{"op": "tool_call_delta", "content_index": event.ContentIndex, "delta": event.Delta}
	case "toolcall_end":
		return map[string]any{"op": "tool_call_done", "content_index": event.ContentIndex, "tool_call": event.ToolCall}
	case "done":
		delta := map[string]any{"op": "done", "stop_reason": event.Reason}
		if event.Message != nil {
			delta["text"] = AssistantText(*event.Message)
			delta["usage"] = event.Message.Usage
			delta["response_id"] = event.Message.ResponseID
		}
		return delta
	case "error":
		delta := map[string]any{"op": "error", "stop_reason": event.Reason}
		if event.Error != nil {
			delta["message"] = event.Error.ErrorMessage
		}
		return delta
	default:
		return map[string]any{"op": event.Type, "content_index": event.ContentIndex, "delta": event.Delta}
	}
}
