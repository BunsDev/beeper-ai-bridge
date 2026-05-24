package agui

import (
	"fmt"
	"strings"
)

func ValidateEvent(evt Event) error {
	eventType, _ := evt["type"].(string)
	if eventType == "" {
		return fmt.Errorf("ag-ui event missing type")
	}
	if _, ok := evt["timestamp"]; !ok {
		return fmt.Errorf("%s missing timestamp", eventType)
	}
	switch eventType {
	case EventRunStarted:
		return require(evt, "threadId", "runId")
	case EventRunFinished:
		return require(evt, "threadId", "runId", "finishReason")
	case EventRunError:
		return require(evt, "message")
	case EventTextMessageStart:
		return require(evt, "messageId", "role")
	case EventTextMessageContent:
		if err := require(evt, "messageId"); err != nil {
			return err
		}
		return requireStringField(evt, "delta")
	case EventTextMessageEnd:
		return require(evt, "messageId")
	case EventReasoningStart, EventReasoningEnd, EventReasoningMsgStart, EventReasoningMsgEnd:
		return require(evt, "messageId")
	case EventReasoningMsgCont:
		if err := require(evt, "messageId"); err != nil {
			return err
		}
		return requireStringField(evt, "delta")
	case EventToolCallStart:
		if err := require(evt, "toolCallId", "toolCallName"); err != nil {
			return err
		}
		if approval, ok := evt["approval"]; ok {
			if err := validateToolApproval(approval); err != nil {
				return fmt.Errorf("%s has invalid approval: %w", evt["type"], err)
			}
		}
		return validateStringSet(evt, "state", true, validToolStates)
	case EventToolCallArgs:
		if err := require(evt, "toolCallId"); err != nil {
			return err
		}
		if err := requireStringField(evt, "delta"); err != nil {
			return err
		}
		if err := validateStringSet(evt, "state", false, validToolStates); err != nil {
			return err
		}
		if args, ok := evt["args"]; ok {
			if _, ok := args.(string); !ok {
				return fmt.Errorf("%s has invalid args %T", evt["type"], args)
			}
		}
		return nil
	case EventToolCallEnd:
		if err := require(evt, "toolCallId"); err != nil {
			return err
		}
		if result, ok := evt["result"]; ok {
			if _, ok := result.(string); !ok {
				return fmt.Errorf("%s has invalid result %T", evt["type"], result)
			}
		}
		return validateStringSet(evt, "state", true, validToolStates)
	case EventToolCallResult:
		if err := require(evt, "messageId", "toolCallId", "content"); err != nil {
			return err
		}
		return validateStringSet(evt, "state", false, validToolResultStates)
	case EventStepStarted, EventStepFinished:
		return require(evt, "stepName")
	case EventStateSnapshot:
		return require(evt, "snapshot")
	case EventStateDelta:
		return require(evt, "delta")
	case EventMessagesSnapshot:
		return require(evt, "messages")
	case EventCustom:
		return require(evt, "name")
	default:
		return fmt.Errorf("unsupported ag-ui event type %q", eventType)
	}
}

func validateToolApproval(value any) error {
	switch approval := value.(type) {
	case ToolApproval:
		if strings.TrimSpace(approval.ID) == "" {
			return fmt.Errorf("missing id")
		}
		if !approval.NeedsApproval {
			return fmt.Errorf("needsApproval must be true")
		}
		return nil
	case *ToolApproval:
		if approval == nil {
			return fmt.Errorf("missing approval")
		}
		return validateToolApproval(*approval)
	case map[string]any:
		id, _ := approval["id"].(string)
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("missing id")
		}
		if approval["needsApproval"] != true {
			return fmt.Errorf("needsApproval must be true")
		}
		return nil
	default:
		return fmt.Errorf("unexpected %T", value)
	}
}

func ValidateEventSequence(events []Event) error {
	seenRunStart := false
	terminal := false
	textOpen := map[string]bool{}
	reasoningOpen := map[string]bool{}
	toolStarted := map[string]bool{}
	toolEnded := map[string]bool{}

	for i, evt := range events {
		if err := ValidateEvent(evt); err != nil {
			return fmt.Errorf("event %d: %w", i+1, err)
		}
		eventType, _ := evt["type"].(string)
		if terminal {
			return fmt.Errorf("event %d: %s after terminal run event", i+1, eventType)
		}

		switch eventType {
		case EventRunStarted:
			if seenRunStart {
				return fmt.Errorf("event %d: duplicate RUN_STARTED", i+1)
			}
			seenRunStart = true
		case EventRunFinished:
			if !seenRunStart {
				return fmt.Errorf("event %d: RUN_FINISHED before RUN_STARTED", i+1)
			}
			terminal = true
		case EventRunError:
			terminal = true
		case EventTextMessageStart:
			messageID := stringField(evt, "messageId")
			if textOpen[messageID] {
				return fmt.Errorf("event %d: duplicate TEXT_MESSAGE_START for %s", i+1, messageID)
			}
			textOpen[messageID] = true
		case EventTextMessageContent:
			messageID := stringField(evt, "messageId")
			if !textOpen[messageID] {
				return fmt.Errorf("event %d: TEXT_MESSAGE_CONTENT before TEXT_MESSAGE_START for %s", i+1, messageID)
			}
		case EventTextMessageEnd:
			messageID := stringField(evt, "messageId")
			if !textOpen[messageID] {
				return fmt.Errorf("event %d: TEXT_MESSAGE_END before TEXT_MESSAGE_START for %s", i+1, messageID)
			}
			delete(textOpen, messageID)
		case EventReasoningMsgStart:
			messageID := stringField(evt, "messageId")
			if reasoningOpen[messageID] {
				return fmt.Errorf("event %d: duplicate REASONING_MESSAGE_START for %s", i+1, messageID)
			}
			reasoningOpen[messageID] = true
		case EventReasoningMsgCont:
			messageID := stringField(evt, "messageId")
			if !reasoningOpen[messageID] {
				return fmt.Errorf("event %d: REASONING_MESSAGE_CONTENT before REASONING_MESSAGE_START for %s", i+1, messageID)
			}
		case EventReasoningMsgEnd:
			messageID := stringField(evt, "messageId")
			if !reasoningOpen[messageID] {
				return fmt.Errorf("event %d: REASONING_MESSAGE_END before REASONING_MESSAGE_START for %s", i+1, messageID)
			}
			delete(reasoningOpen, messageID)
		case EventToolCallStart:
			toolCallID := stringField(evt, "toolCallId")
			if toolStarted[toolCallID] {
				return fmt.Errorf("event %d: duplicate TOOL_CALL_START for %s", i+1, toolCallID)
			}
			toolStarted[toolCallID] = true
		case EventToolCallArgs:
			toolCallID := stringField(evt, "toolCallId")
			if !toolStarted[toolCallID] {
				return fmt.Errorf("event %d: TOOL_CALL_ARGS before TOOL_CALL_START for %s", i+1, toolCallID)
			}
		case EventToolCallEnd:
			toolCallID := stringField(evt, "toolCallId")
			if !toolStarted[toolCallID] {
				return fmt.Errorf("event %d: TOOL_CALL_END before TOOL_CALL_START for %s", i+1, toolCallID)
			}
			if toolEnded[toolCallID] {
				return fmt.Errorf("event %d: duplicate TOOL_CALL_END for %s", i+1, toolCallID)
			}
			toolEnded[toolCallID] = true
		case EventToolCallResult:
			toolCallID := stringField(evt, "toolCallId")
			if !toolStarted[toolCallID] {
				return fmt.Errorf("event %d: TOOL_CALL_RESULT before TOOL_CALL_START for %s", i+1, toolCallID)
			}
		}
	}
	return nil
}

var validToolStates = map[string]bool{
	ToolStateAwaitingInput:     true,
	ToolStateInputStreaming:    true,
	ToolStateInputComplete:     true,
	ToolStateApprovalRequested: true,
	ToolStateApprovalResponded: true,
}

func stringField(evt Event, key string) string {
	value, _ := evt[key].(string)
	return value
}

var validToolResultStates = map[string]bool{
	ToolResultStateStreaming: true,
	ToolResultStateComplete:  true,
	ToolResultStateError:     true,
}

func validateStringSet(evt Event, key string, required bool, allowed map[string]bool) error {
	value, ok := evt[key]
	if !ok || value == nil {
		if required {
			return fmt.Errorf("%s missing %s", evt["type"], key)
		}
		return nil
	}
	stringValue, ok := value.(string)
	if !ok || !allowed[stringValue] {
		return fmt.Errorf("%s has invalid %s %q", evt["type"], key, value)
	}
	return nil
}
