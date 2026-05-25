package aistream

import (
	"fmt"
	"strings"

	"github.com/beeper/ai-bridge/pkg/ag-ui"
)

func (t Run) Text() string {
	var out strings.Builder
	for _, evt := range t.Events {
		if evt["type"] == agui.EventTextMessageContent {
			if delta, _ := evt["delta"].(string); delta != "" {
				out.WriteString(delta)
			}
		}
	}
	return out.String()
}

func (t Run) FinalUIMessage(textBudget int, includeThinking bool) agui.UIMessage {
	message := agui.UIMessage{
		ID:       t.MessageID,
		Role:     agui.RoleAssistant,
		Metadata: t.UIMessageMetadata(true).Map(),
	}
	var textPart agui.MessagePart
	var thinkingPart agui.MessagePart
	var textContent, thinkingContent strings.Builder
	toolParts := map[string]agui.MessagePart{}
	approvalByID := map[string]any{}
	appendPart := func(part agui.MessagePart) agui.MessagePart {
		message.Parts = append(message.Parts, part)
		return part
	}
	for _, evt := range t.Events {
		switch evt["type"] {
		case agui.EventTextMessageContent:
			delta, _ := evt["delta"].(string)
			if delta == "" {
				continue
			}
			if textPart == nil {
				textPart = appendPart(agui.MessagePart{"type": "text", "content": "", "state": agui.PartStateStreaming})
			}
			textContent.WriteString(delta)
		case agui.EventTextMessageEnd:
			if textPart != nil {
				textPart["state"] = agui.PartStateDone
			}
		case agui.EventReasoningMsgCont:
			delta, _ := evt["delta"].(string)
			if delta == "" {
				continue
			}
			if !includeThinking {
				continue
			}
			if thinkingPart == nil {
				thinkingPart = appendPart(agui.MessagePart{"type": "thinking", "content": "", "state": agui.PartStateStreaming})
			}
			thinkingContent.WriteString(delta)
		case agui.EventReasoningMsgEnd:
			if thinkingPart != nil {
				thinkingPart["state"] = agui.PartStateDone
			}
		case agui.EventToolCallStart:
			toolCallID, _ := evt["toolCallId"].(string)
			if toolCallID == "" {
				continue
			}
			part := agui.MessagePart{
				"type":       "tool-call",
				"id":         toolCallID,
				"toolCallId": toolCallID,
				"name":       firstString(evt["toolName"], evt["toolCallName"]),
				"arguments":  "",
				"state":      firstString(evt["state"]),
			}
			if index, ok := evt["index"]; ok {
				part["index"] = index
			}
			if approval, ok := evt["approval"]; ok {
				part["approval"] = approval
			}
			if metadata, ok := evt["metadata"]; ok {
				part["metadata"] = metadata
			}
			toolParts[toolCallID] = appendPart(part)
		case agui.EventToolCallArgs:
			toolCallID, _ := evt["toolCallId"].(string)
			part := toolParts[toolCallID]
			if part == nil {
				part = appendPart(agui.MessagePart{"type": "tool-call", "id": toolCallID, "toolCallId": toolCallID, "arguments": ""})
				toolParts[toolCallID] = part
			}
			part["state"] = firstString(evt["state"])
			if delta, _ := evt["delta"].(string); delta != "" {
				part["arguments"] = asString(part["arguments"]) + delta
			}
			if args, ok := evt["args"]; ok {
				part["input"] = args
			}
		case agui.EventToolCallEnd:
			toolCallID, _ := evt["toolCallId"].(string)
			part := toolParts[toolCallID]
			if part == nil {
				part = appendPart(agui.MessagePart{"type": "tool-call", "id": toolCallID, "toolCallId": toolCallID})
				toolParts[toolCallID] = part
			}
			part["name"] = firstString(part["name"], evt["toolName"], evt["toolCallName"])
			part["state"] = firstString(evt["state"])
			if input, ok := evt["input"]; ok {
				part["input"] = input
			}
			if result, ok := evt["result"]; ok {
				part["output"] = jsonValue(result)
			}
		case agui.EventToolCallResult:
			toolCallID, _ := evt["toolCallId"].(string)
			if toolCallID == "" {
				continue
			}
			part := toolParts[toolCallID]
			if part == nil {
				part = appendPart(agui.MessagePart{"type": "tool-call", "id": toolCallID, "toolCallId": toolCallID})
				toolParts[toolCallID] = part
			}
			part["state"] = agui.ToolStateInputComplete
			content := asString(evt["content"])
			if previous := asString(part["output"]); previous != "" {
				content = previous + content
			}
			if content != "" {
				part["output"] = toolResultOutput(content, firstString(evt["state"]), evt["error"])
			}
		case agui.EventCustom:
			name, _ := evt["name"].(string)
			value, _ := evt["value"].(map[string]any)
			switch name {
			case agui.ApprovalCustomRequested:
				if toolCallID, _ := value["toolCallId"].(string); toolCallID != "" {
					if part := toolParts[toolCallID]; part != nil {
						part["approval"] = value["approval"]
						part["state"] = agui.ToolStateApprovalRequested
					}
				}
			case agui.ApprovalCustomResponded:
				if approval, ok := value["approval"]; ok {
					approvalByID[approvalMapID(approval)] = approval
				}
			case "com.beeper.source":
				part := cloneValueMap(value)
				part["type"] = "source-url"
				if asString(part["sourceId"]) == "" {
					part["sourceId"] = firstString(part["url"], part["title"])
				}
				message.Parts = append(message.Parts, part)
			case "com.beeper.document":
				part := cloneValueMap(value)
				part["type"] = "source-document"
				if asString(part["sourceId"]) == "" {
					part["sourceId"] = firstString(part["id"], part["title"])
				}
				message.Parts = append(message.Parts, part)
			case "com.beeper.file":
				part := cloneValueMap(value)
				part["type"] = "file"
				message.Parts = append(message.Parts, part)
			case "com.beeper.data":
				message.Parts = append(message.Parts, agui.MessagePart{"type": "data-com-beeper-data", "data": value})
			}
		}
	}
	for _, part := range toolParts {
		if approvalID := approvalMapID(part["approval"]); approvalID != "" {
			if response := approvalByID[approvalID]; response != nil {
				part["approvalResponse"] = response
				part["state"] = agui.ToolStateApprovalResponded
			}
		}
	}
	if t.Status.State != "" && t.Status.State != "streaming" {
		for _, part := range toolParts {
			finalizeOpenToolPart(part, t.Status.State)
		}
	}
	if textPart != nil {
		textPart["content"] = textContent.String()
	}
	if thinkingPart != nil {
		thinkingPart["content"] = thinkingContent.String()
	}
	compactTextPart(textPart, textBudget)
	compactTextPart(thinkingPart, textBudget)
	if len(message.Parts) > 1 {
		visible := make([]agui.MessagePart, 0, len(message.Parts))
		other := make([]agui.MessagePart, 0, len(message.Parts))
		for _, part := range message.Parts {
			switch part["type"] {
			case "text", "thinking":
				visible = append(visible, part)
			default:
				other = append(other, part)
			}
		}
		if len(visible) > 0 {
			message.Parts = append(visible, other...)
		}
	}
	return message
}

func toolResultOutput(content string, state string, err any) any {
	output := jsonValue(content)
	result, ok := output.(map[string]any)
	if !ok {
		if state == agui.ToolResultStateError {
			reason := asString(err)
			if reason == "" {
				reason = content
			}
			return map[string]any{
				"state":  agui.ToolResultStateError,
				"status": "failed",
				"reason": reason,
			}
		}
		return output
	}
	if state == agui.ToolResultStateError {
		if result["state"] == nil {
			result["state"] = agui.ToolResultStateError
		}
		if result["status"] == nil {
			result["status"] = "failed"
		}
		if result["reason"] == nil && err != nil {
			result["reason"] = asString(err)
		}
	} else if state == agui.ToolResultStateComplete {
		if result["state"] == nil {
			result["state"] = agui.ToolResultStateComplete
		}
		if result["status"] == nil {
			result["status"] = "success"
		}
	}
	return result
}

func finalizeOpenToolPart(part agui.MessagePart, runState string) {
	if part == nil {
		return
	}
	if _, hasOutput := part["output"]; hasOutput {
		return
	}
	state, _ := part["state"].(string)
	switch state {
	case agui.ToolStateApprovalResponded:
		return
	}
	reason := "run finalized before tool completed"
	if runState == "aborted" {
		reason = "run aborted before tool completed"
	} else if runState == "error" {
		reason = "run failed before tool completed"
	}
	part["state"] = agui.ToolStateInputComplete
	part["output"] = map[string]any{
		"state":  agui.ToolResultStateError,
		"status": "failed",
		"reason": reason,
	}
}

func (t Run) InitialUIMessage() agui.UIMessage {
	message := agui.UIMessage{
		ID:       t.MessageID,
		Role:     agui.RoleAssistant,
		Metadata: t.UIMessageMetadata(false).Map(),
	}
	if t.Preview.Text != "" {
		message.Parts = []agui.MessagePart{{
			"type":    "text",
			"content": t.Preview.Text,
			"state":   agui.PartStateStreaming,
		}}
	} else {
		message.Parts = []agui.MessagePart{}
	}
	return message
}

func (t Run) UIMessageMetadata(includeUsage bool) UIMessageMetadata {
	metadata := UIMessageMetadata{
		ThreadID: t.ThreadID,
		RunID:    t.RunID,
		Status:   t.Status,
	}
	if includeUsage {
		metadata.Usage = &t.Usage
	}
	return metadata
}

func compactTextPart(part agui.MessagePart, budget int) {
	if part == nil {
		return
	}
	content, _ := part["content"].(string)
	if budget <= 0 {
		if part["state"] == "" {
			part["state"] = agui.PartStateDone
		}
		return
	}
	preview := BoundedPreview(content, budget)
	part["content"] = preview
	if len(preview) < len(content) {
		part["providerMetadata"] = map[string]any{"truncated": true}
	}
	if part["state"] == "" {
		part["state"] = agui.PartStateDone
	}
}

func asString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func cloneValueMap(value map[string]any) agui.MessagePart {
	cp := make(agui.MessagePart, len(value)+1)
	for key, item := range value {
		cp[key] = item
	}
	return cp
}

func firstString(values ...any) string {
	for _, value := range values {
		if text, ok := value.(string); ok && text != "" {
			return text
		}
	}
	return ""
}

func approvalMapID(value any) string {
	switch typed := value.(type) {
	case agui.ToolApproval:
		return typed.ID
	case *agui.ToolApproval:
		if typed != nil {
			return typed.ID
		}
	case agui.ToolApprovalResponse:
		return typed.ID
	case *agui.ToolApprovalResponse:
		if typed != nil {
			return typed.ID
		}
	case map[string]any:
		id, _ := typed["id"].(string)
		return id
	}
	return ""
}
