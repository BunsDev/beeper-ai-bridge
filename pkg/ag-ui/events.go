package agui

import (
	"strings"
	"time"
)

const (
	EventRunStarted         = "RUN_STARTED"
	EventRunFinished        = "RUN_FINISHED"
	EventRunError           = "RUN_ERROR"
	EventTextMessageStart   = "TEXT_MESSAGE_START"
	EventTextMessageContent = "TEXT_MESSAGE_CONTENT"
	EventTextMessageEnd     = "TEXT_MESSAGE_END"
	EventToolCallStart      = "TOOL_CALL_START"
	EventToolCallArgs       = "TOOL_CALL_ARGS"
	EventToolCallEnd        = "TOOL_CALL_END"
	EventToolCallResult     = "TOOL_CALL_RESULT"
	EventStepStarted        = "STEP_STARTED"
	EventStepFinished       = "STEP_FINISHED"
	EventStateSnapshot      = "STATE_SNAPSHOT"
	EventStateDelta         = "STATE_DELTA"
	EventMessagesSnapshot   = "MESSAGES_SNAPSHOT"
	EventCustom             = "CUSTOM"
	EventReasoningStart     = "REASONING_START"
	EventReasoningEnd       = "REASONING_END"
	EventReasoningMsgStart  = "REASONING_MESSAGE_START"
	EventReasoningMsgCont   = "REASONING_MESSAGE_CONTENT"
	EventReasoningMsgEnd    = "REASONING_MESSAGE_END"
)

const (
	RoleAssistant = "assistant"
	RoleUser      = "user"
	RoleSystem    = "system"
	RoleTool      = "tool"
)

const (
	ToolStateAwaitingInput     = "awaiting-input"
	ToolStateInputStreaming    = "input-streaming"
	ToolStateInputComplete     = "input-complete"
	ToolStateApprovalRequested = "approval-requested"
	ToolStateApprovalResponded = "approval-responded"
	ToolResultStateStreaming   = "streaming"
	ToolResultStateComplete    = "complete"
	ToolResultStateError       = "error"
	PartStateStreaming         = "streaming"
	PartStateDone              = "done"
	ApprovalCustomRequested    = "approval-requested"
	ApprovalCustomResponded    = "approval-responded"
	FinishReasonStop           = "stop"
	FinishReasonLength         = "length"
	FinishReasonContentFilter  = "content_filter"
	FinishReasonToolCalls      = "tool_calls"
	FinishReasonOther          = "other"
)

type Event map[string]any

type UIMessage struct {
	ID        string         `json:"id"`
	Role      string         `json:"role"`
	Parts     []MessagePart  `json:"parts"`
	CreatedAt *time.Time     `json:"createdAt,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type MessagePart map[string]any

type RunAgentInput struct {
	ThreadID       string         `json:"threadId,omitempty"`
	RunID          string         `json:"runId,omitempty"`
	State          map[string]any `json:"state,omitempty"`
	Messages       []UIMessage    `json:"messages,omitempty"`
	Tools          []Tool         `json:"tools,omitempty"`
	Context        []ContextItem  `json:"context,omitempty"`
	ForwardedProps map[string]any `json:"forwardedProps,omitempty"`
	Data           map[string]any `json:"data,omitempty"`
}

type Tool struct {
	Name          string         `json:"name"`
	Description   string         `json:"description,omitempty"`
	InputSchema   map[string]any `json:"inputSchema,omitempty"`
	OutputSchema  map[string]any `json:"outputSchema,omitempty"`
	NeedsApproval bool           `json:"needsApproval,omitempty"`
}

type ContextItem struct {
	Type  string         `json:"type"`
	Value any            `json:"value,omitempty"`
	Meta  map[string]any `json:"meta,omitempty"`
}

type ToolApproval struct {
	ID            string         `json:"id"`
	NeedsApproval bool           `json:"needsApproval"`
	Fields        map[string]any `json:"fields,omitempty"`
}

type ToolApprovalResponse struct {
	ID       string         `json:"id"`
	Approved bool           `json:"approved"`
	Always   bool           `json:"always,omitempty"`
	Reason   string         `json:"reason,omitempty"`
	Fields   map[string]any `json:"fields,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"promptTokens,omitempty"`
	CompletionTokens int `json:"completionTokens,omitempty"`
	TotalTokens      int `json:"totalTokens,omitempty"`
}

type EventBuilder struct {
	now   func() time.Time
	model string
}

func NewEventBuilder(model string, now func() time.Time) EventBuilder {
	if now == nil {
		now = time.Now
	}
	return EventBuilder{now: now, model: strings.TrimSpace(model)}
}

func (b EventBuilder) base(eventType string) Event {
	evt := Event{
		"type":      eventType,
		"timestamp": b.now().UnixMilli(),
	}
	if b.model != "" {
		evt["model"] = b.model
	}
	return evt
}

func (b EventBuilder) RunStarted(threadID, runID string) Event {
	evt := b.base(EventRunStarted)
	evt["threadId"] = threadID
	evt["runId"] = runID
	return evt
}

func (b EventBuilder) RunFinished(threadID, runID, finishReason string, usage Usage) Event {
	evt := b.base(EventRunFinished)
	evt["threadId"] = threadID
	evt["runId"] = runID
	evt["finishReason"] = NormalizeFinishReason(finishReason)
	evt["usage"] = usage
	return evt
}

func (b EventBuilder) RunError(threadID, runID, message string) Event {
	evt := b.base(EventRunError)
	evt["threadId"] = threadID
	if strings.TrimSpace(runID) != "" {
		evt["runId"] = runID
	}
	evt["message"] = message
	evt["error"] = map[string]any{"message": message}
	return evt
}

func (b EventBuilder) TextMessageStart(messageID, role string) Event {
	if role == "" {
		role = RoleAssistant
	}
	evt := b.base(EventTextMessageStart)
	evt["messageId"] = messageID
	evt["role"] = role
	return evt
}

func (b EventBuilder) TextMessageContent(messageID, delta string) Event {
	evt := b.base(EventTextMessageContent)
	evt["messageId"] = messageID
	evt["delta"] = delta
	return evt
}

func (b EventBuilder) TextMessageEnd(messageID string) Event {
	evt := b.base(EventTextMessageEnd)
	evt["messageId"] = messageID
	return evt
}

func (b EventBuilder) ReasoningStart(messageID string) Event {
	evt := b.base(EventReasoningStart)
	evt["messageId"] = messageID
	return evt
}

func (b EventBuilder) ReasoningEnd(messageID string) Event {
	evt := b.base(EventReasoningEnd)
	evt["messageId"] = messageID
	return evt
}

func (b EventBuilder) ReasoningMessageStart(messageID string) Event {
	evt := b.base(EventReasoningMsgStart)
	evt["messageId"] = messageID
	return evt
}

func (b EventBuilder) ReasoningMessageContent(messageID, delta string) Event {
	evt := b.base(EventReasoningMsgCont)
	evt["messageId"] = messageID
	evt["delta"] = delta
	return evt
}

func (b EventBuilder) ReasoningMessageEnd(messageID string) Event {
	evt := b.base(EventReasoningMsgEnd)
	evt["messageId"] = messageID
	return evt
}

func (b EventBuilder) ToolCallStart(messageID, toolCallID, name string, index *int, approval *ToolApproval) Event {
	return b.ToolCallStartWithMetadata(messageID, toolCallID, name, index, approval, nil)
}

func (b EventBuilder) ToolCallStartWithMetadata(messageID, toolCallID, name string, index *int, approval *ToolApproval, metadata map[string]any) Event {
	evt := b.base(EventToolCallStart)
	if messageID != "" {
		evt["parentMessageId"] = messageID
	}
	evt["toolCallId"] = toolCallID
	evt["toolCallName"] = name
	evt["toolName"] = name
	if len(metadata) > 0 {
		evt["metadata"] = metadata
	}
	if index != nil {
		evt["index"] = *index
	}
	if approval != nil {
		evt["approval"] = approval
		evt["state"] = ToolStateApprovalRequested
	} else {
		evt["state"] = ToolStateAwaitingInput
	}
	return evt
}

func (b EventBuilder) ToolCallArgs(toolCallID, delta string, args any) Event {
	evt := b.base(EventToolCallArgs)
	evt["toolCallId"] = toolCallID
	evt["delta"] = delta
	evt["state"] = ToolStateInputStreaming
	if args != nil {
		evt["args"] = args
	}
	return evt
}

func (b EventBuilder) ToolCallEnd(toolCallID, name string, input, result any, state string) Event {
	evt := b.base(EventToolCallEnd)
	evt["toolCallId"] = toolCallID
	evt["toolCallName"] = name
	evt["toolName"] = name
	if input != nil {
		evt["input"] = input
	}
	if result != nil {
		evt["result"] = result
	}
	if state == "" {
		state = ToolStateInputComplete
	}
	evt["state"] = state
	return evt
}

func (b EventBuilder) ToolCallResult(messageID, toolCallID, content, state, role string) Event {
	if role == "" {
		role = RoleTool
	}
	if state == "" {
		state = ToolResultStateComplete
	}
	evt := b.base(EventToolCallResult)
	evt["messageId"] = messageID
	evt["toolCallId"] = toolCallID
	evt["content"] = content
	evt["state"] = state
	evt["role"] = role
	return evt
}

func (b EventBuilder) StepStarted(messageID, stepName string) Event {
	if stepName == "" {
		panic("ag-ui: stepName is required for STEP_STARTED")
	}
	evt := b.base(EventStepStarted)
	if messageID != "" {
		evt["messageId"] = messageID
	}
	evt["stepName"] = stepName
	return evt
}

func (b EventBuilder) StepFinished(messageID, stepName string) Event {
	if stepName == "" {
		panic("ag-ui: stepName is required for STEP_FINISHED")
	}
	evt := b.base(EventStepFinished)
	if messageID != "" {
		evt["messageId"] = messageID
	}
	evt["stepName"] = stepName
	return evt
}

func (b EventBuilder) StateSnapshot(state map[string]any) Event {
	evt := b.base(EventStateSnapshot)
	evt["snapshot"] = state
	return evt
}

func (b EventBuilder) StateDelta(delta any) Event {
	evt := b.base(EventStateDelta)
	evt["delta"] = delta
	return evt
}

func (b EventBuilder) MessagesSnapshot(messages []UIMessage) Event {
	evt := b.base(EventMessagesSnapshot)
	evt["messages"] = messages
	return evt
}

func (b EventBuilder) Custom(name string, value any) Event {
	evt := b.base(EventCustom)
	evt["name"] = name
	evt["value"] = value
	return evt
}

func TextPart(content string) MessagePart {
	return MessagePart{"type": "text", "content": content}
}

func ThinkingPart(content string) MessagePart {
	return MessagePart{"type": "thinking", "content": content}
}

func ToolCallPart(id, name string, arguments any, state string, approval *ToolApproval, output any) MessagePart {
	part := MessagePart{"type": "tool-call", "id": id, "name": name, "arguments": arguments, "state": state}
	if approval != nil {
		part["approval"] = approval
	}
	if output != nil {
		part["output"] = output
	}
	return part
}

func ToolResultPart(toolCallID string, content any, state string, err any) MessagePart {
	part := MessagePart{"type": "tool-result", "toolCallId": toolCallID, "content": content, "state": state}
	if err != nil {
		part["error"] = err
	}
	return part
}
