package agui

import (
	"testing"
	"time"
)

func TestBuildersCoverLifecycleEventsWithTimestamps(t *testing.T) {
	now := func() time.Time { return time.Unix(10, 0) }
	builder := NewEventBuilder("dummy/model", now)
	idx := 1
	events := []Event{
		builder.RunStarted("thread", "run"),
		builder.RunFinished("thread", "run", "tool-calls", Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}),
		builder.RunError("thread", "run", "failed"),
		builder.TextMessageStart("msg", RoleAssistant),
		builder.TextMessageContent("msg", "hello"),
		builder.TextMessageEnd("msg"),
		builder.TextMessageChunk("msg", RoleAssistant, "hello"),
		builder.ReasoningStart("msg"),
		builder.ReasoningMessageStart("msg"),
		builder.ReasoningMessageContent("msg", "thinking"),
		builder.ReasoningMessageEnd("msg"),
		builder.ReasoningEnd("msg"),
		builder.ReasoningMessageChunk("msg", "thinking"),
		builder.ReasoningEncryptedValue("message", "msg", "encrypted"),
		builder.ToolCallStart("msg", "tool", "search", &idx, &ToolApproval{ID: "approval", NeedsApproval: true}),
		builder.ToolCallArgs("tool", `{"q":"he`, nil),
		builder.ToolCallEnd("tool", "search", map[string]any{"q": "hello"}, `{"ok":true}`, ToolStateInputComplete),
		builder.ToolCallChunk("tool", "search", "msg", `{"q":"he`),
		builder.ToolCallResult("msg", "tool", `{"ok":true}`, ToolResultStateComplete, RoleTool),
		builder.StepStarted("msg", "step"),
		builder.StepFinished("msg", "step"),
		builder.StateSnapshot(map[string]any{"open": true}),
		builder.StateDelta(map[string]any{"path": "/open", "value": false}),
		builder.MessagesSnapshot([]UIMessage{{ID: "msg", Role: RoleAssistant, Parts: []MessagePart{TextPart("hello")}}}),
		builder.ActivitySnapshot("msg", "thinking", map[string]any{"text": "working"}, nil),
		builder.ActivityDelta("msg", "thinking", []any{map[string]any{"op": "add", "path": "/text", "value": "working"}}),
		builder.Raw(map[string]any{"type": "provider.event"}, "openai"),
		builder.Custom("com.beeper.test", map[string]any{"ok": true}),
	}
	for _, evt := range events {
		if err := ValidateEvent(evt); err != nil {
			t.Fatalf("ValidateEvent(%s) returned error: %v", evt["type"], err)
		}
		if evt["timestamp"] == nil {
			t.Fatalf("event missing timestamp: %#v", evt)
		}
	}
	if got := events[1]["finishReason"]; got != FinishReasonToolCalls {
		t.Fatalf("finish reason = %q, want %q", got, FinishReasonToolCalls)
	}
	if got := events[2]["message"]; got != "failed" {
		t.Fatalf("run error message = %#v, want failed", got)
	}
	toolStart := events[14]
	if got := toolStart["index"]; got != 1 {
		t.Fatalf("tool index = %#v, want 1", got)
	}
	if got := toolStart["parentMessageId"]; got != "msg" {
		t.Fatalf("tool parentMessageId = %#v, want msg", got)
	}
	if _, hasMessageID := toolStart["messageId"]; hasMessageID {
		t.Fatalf("tool start should not emit deprecated messageId: %#v", toolStart)
	}
	if _, hasSnapshot := events[21]["snapshot"]; !hasSnapshot {
		t.Fatalf("state snapshot should emit snapshot field: %#v", events[21])
	}
}

func TestValidateRejectsBadEvents(t *testing.T) {
	tests := []Event{
		{},
		{"type": EventRunStarted, "threadId": "thread"},
		{"type": EventRunError, "threadId": "thread", "error": map[string]any{"message": "failed"}},
		{"type": EventTextMessageContent, "messageId": "msg"},
		{"type": "REASONING_MESSAGE_CONTENT"},
		{"type": EventReasoningEncrypted, "subtype": "bad", "entityId": "msg", "encryptedValue": "x"},
		{"type": EventToolCallStart, "toolCallId": "tool", "toolCallName": "search", "state": "output-available"},
		{"type": EventToolCallStart, "toolCallId": "tool", "toolCallName": "search", "state": ToolStateApprovalRequested, "approval": ToolApproval{ID: "", NeedsApproval: true}},
		{"type": EventToolCallStart, "toolCallId": "tool", "toolCallName": "search", "state": ToolStateApprovalRequested, "approval": map[string]any{"id": "approval", "needsApproval": false}},
		{"type": EventToolCallArgs, "toolCallId": "tool", "delta": "{}", "args": map[string]any{"bad": true}},
		{"type": EventToolCallEnd, "toolCallId": "tool", "result": map[string]any{"bad": true}, "state": ToolStateInputComplete},
		{"type": EventToolCallResult, "messageId": "msg", "toolCallId": "tool", "content": "{}", "state": "output-error"},
		{"type": EventStepStarted, "stepId": "deprecated-only"},
		{"type": EventStateSnapshot, "state": map[string]any{}},
		{"type": EventActivitySnapshot, "messageId": "msg", "activityType": "thinking"},
		{"type": EventActivityDelta, "messageId": "msg", "activityType": "thinking"},
		{"type": EventRaw},
	}
	for _, evt := range tests {
		if err := ValidateEvent(evt); err == nil {
			t.Fatalf("expected validation error for %#v", evt)
		}
	}
}

func TestValidateEventSequenceRejectsBadOrdering(t *testing.T) {
	now := func() time.Time { return time.Unix(10, 0) }
	builder := NewEventBuilder("dummy/model", now)

	valid := []Event{
		builder.RunStarted("thread", "run"),
		builder.TextMessageStart("msg", RoleAssistant),
		builder.TextMessageContent("msg", "hello"),
		builder.TextMessageEnd("msg"),
		builder.ToolCallStart("msg", "tool", "search", nil, nil),
		builder.ToolCallArgs("tool", `{"q":"hello"}`, `{"q":"hello"}`),
		builder.ToolCallEnd("tool", "search", map[string]any{"q": "hello"}, `{"ok":true}`, ToolStateInputComplete),
		builder.RunFinished("thread", "run", FinishReasonStop, Usage{}),
	}
	if err := ValidateEventSequence(valid); err != nil {
		t.Fatalf("ValidateEventSequence(valid) returned error: %v", err)
	}

	tests := [][]Event{
		{builder.TextMessageContent("msg", "hello")},
		{builder.ReasoningMessageContent("msg", "thinking")},
		{builder.ToolCallArgs("tool", "{}", "{}")},
		{builder.ToolCallResult("msg", "tool", "{}", ToolResultStateComplete, RoleTool)},
		{
			builder.RunStarted("thread", "run"),
			builder.RunFinished("thread", "run", FinishReasonStop, Usage{}),
			builder.TextMessageStart("msg", RoleAssistant),
		},
	}
	for _, events := range tests {
		if err := ValidateEventSequence(events); err == nil {
			t.Fatalf("expected ordering error for %#v", events)
		}
	}
}

func TestRunAgentInputModelsBidirectionalShape(t *testing.T) {
	input := RunAgentInput{
		ThreadID: "thread",
		RunID:    "run",
		State:    map[string]any{"open": true},
		Messages: []UIMessage{{
			ID:    "msg",
			Role:  RoleUser,
			Parts: []MessagePart{TextPart("hello")},
		}},
		Tools: []Tool{{Name: "send_email", NeedsApproval: true}},
		Context: []ContextItem{{
			Type:  "beeper-room",
			Value: "room",
		}},
		ForwardedProps: map[string]any{"trace": "abc"},
		Data:           map[string]any{"legacy": true},
	}
	if input.ThreadID != "thread" || !input.Tools[0].NeedsApproval || input.ForwardedProps["trace"] != "abc" {
		t.Fatalf("bad RunAgentInput shape: %#v", input)
	}
}
