package aistream

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	agui "github.com/beeper/ai-bridge/pkg/ag-ui"
	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/msgconv"
	"github.com/yuin/goldmark"
	"maunium.net/go/mautrix/event"
)

const (
	AIContentKey         = "com.beeper.ai"
	AIMetadataKey        = "com.beeper.ai.metadata"
	DontRenderEditedKey  = "com.beeper.dont_render_edited"
	PerMessageProfileKey = "com.beeper.per_message_profile"
	LLMStreamDeltasKey   = "com.beeper.llm.deltas"
)

type RunInfo struct {
	AgentDisplayName string
	AgentID          string
	MessageID        string
	ModelID          string
	ProviderID       string
	RunID            string
	ThreadID         string
}

type StreamMapper struct {
	run  RunInfo
	seq  int
	open map[int]string
}

func NewStreamMapper(run RunInfo) *StreamMapper {
	return &StreamMapper{run: normalizeRunInfo(run), open: make(map[int]string)}
}

func InitialMessageExtra(run RunInfo) map[string]any {
	run = normalizeRunInfo(run)
	return map[string]any{
		AIContentKey: map[string]any{
			"id":       run.MessageID,
			"metadata": runMetadata(run, "streaming", "", ai.Usage{}, ""),
			"parts":    []any{},
			"role":     "assistant",
		},
		AIMetadataKey:        metadata(run, "streaming", "", ai.Usage{}, "", false, ""),
		PerMessageProfileKey: perMessageProfile(run),
	}
}

func FinalMessageContent(run RunInfo, message ai.Message) (*event.MessageEventContent, map[string]any, map[string]any) {
	run = normalizeRunInfo(run)
	text := msgconv.AssistantText(message)
	content := msgconv.TextContent(firstNonEmpty(text, "..."))
	if html := markdownHTML(text); html != "" {
		content.Format = event.FormatHTML
		content.FormattedBody = html
	}
	usage := message.Usage
	stopReason := firstNonEmpty(string(message.StopReason), "stop")
	extra := map[string]any{
		AIContentKey: map[string]any{
			"id":       run.MessageID,
			"metadata": runMetadata(run, "complete", stopReason, usage, message.ErrorMessage),
			"parts":    finalParts(message),
			"role":     "assistant",
		},
		AIMetadataKey:        metadata(run, "complete", stopReason, usage, text, len([]rune(text)) > 240, message.ErrorMessage),
		PerMessageProfileKey: perMessageProfile(run),
		"com.beeper.stream":  nil,
	}
	topLevel := map[string]any{
		DontRenderEditedKey:  true,
		PerMessageProfileKey: perMessageProfile(run),
	}
	return content, extra, topLevel
}

func (m *StreamMapper) CarrierContent(evt ai.AssistantMessageEvent, eventID string) map[string]any {
	deltas := m.Deltas(evt, eventID)
	if len(deltas) == 0 {
		return nil
	}
	return map[string]any{LLMStreamDeltasKey: deltas}
}

func (m *StreamMapper) Deltas(evt ai.AssistantMessageEvent, eventID string) []map[string]any {
	parts := m.parts(evt)
	if len(parts) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		m.seq++
		out = append(out, map[string]any{
			"m.relates_to": map[string]any{"event_id": eventID, "rel_type": "m.reference"},
			"part":         part,
			"seq":          m.seq,
			"turn_id":      m.run.RunID,
		})
	}
	return out
}

func (m *StreamMapper) parts(evt ai.AssistantMessageEvent) []map[string]any {
	switch evt.Type {
	case "start":
		return []map[string]any{m.runStarted(), m.textStart()}
	case "text_start":
		m.open[evt.ContentIndex] = "text"
		return []map[string]any{m.textStart()}
	case "text_delta":
		if _, ok := m.open[evt.ContentIndex]; !ok {
			m.open[evt.ContentIndex] = "text"
			return []map[string]any{m.textStart(), m.textContent(evt.Delta)}
		}
		return []map[string]any{m.textContent(evt.Delta)}
	case "text_end":
		delete(m.open, evt.ContentIndex)
		return []map[string]any{m.textEnd()}
	case "thinking_start":
		m.open[evt.ContentIndex] = "thinking"
		return []map[string]any{m.reasoningStart()}
	case "thinking_delta":
		if _, ok := m.open[evt.ContentIndex]; !ok {
			m.open[evt.ContentIndex] = "thinking"
			return []map[string]any{m.reasoningStart(), m.reasoningContent(evt.Delta)}
		}
		return []map[string]any{m.reasoningContent(evt.Delta)}
	case "thinking_end":
		delete(m.open, evt.ContentIndex)
		return []map[string]any{m.reasoningEnd()}
	case "toolcall_start":
		return []map[string]any{toolStart(evt)}
	case "toolcall_delta":
		return []map[string]any{toolArgs(evt)}
	case "toolcall_end":
		return []map[string]any{toolEnd(evt)}
	case "done":
		return append(m.closeOpen(), m.runFinished(string(evt.Reason)))
	case "error":
		return []map[string]any{{"message": errorMessage(evt), "runId": m.run.RunID, "type": agui.RunError}}
	default:
		return nil
	}
}

func (m *StreamMapper) closeOpen() []map[string]any {
	parts := make([]map[string]any, 0, len(m.open)+2)
	for idx, kind := range m.open {
		if kind == "thinking" {
			parts = append(parts, m.reasoningEnd())
		}
		delete(m.open, idx)
	}
	parts = append(parts, m.textEnd())
	return parts
}

func (m *StreamMapper) runStarted() map[string]any {
	return map[string]any{"runId": m.run.RunID, "threadId": m.run.ThreadID, "type": agui.RunStarted}
}

func (m *StreamMapper) runFinished(reason string) map[string]any {
	return map[string]any{"finishReason": firstNonEmpty(reason, "stop"), "runId": m.run.RunID, "threadId": m.run.ThreadID, "type": agui.RunFinished}
}

func (m *StreamMapper) textStart() map[string]any {
	return map[string]any{"messageId": m.run.MessageID, "role": "assistant", "type": agui.TextMessageStart}
}

func (m *StreamMapper) textContent(delta string) map[string]any {
	return map[string]any{"delta": delta, "messageId": m.run.MessageID, "type": agui.TextMessageContent}
}

func (m *StreamMapper) textEnd() map[string]any {
	return map[string]any{"messageId": m.run.MessageID, "type": agui.TextMessageEnd}
}

func (m *StreamMapper) reasoningStart() map[string]any {
	return map[string]any{"messageId": m.run.MessageID, "type": agui.ThinkingTextStart}
}

func (m *StreamMapper) reasoningContent(delta string) map[string]any {
	return map[string]any{"delta": delta, "messageId": m.run.MessageID, "type": agui.ThinkingTextContent}
}

func (m *StreamMapper) reasoningEnd() map[string]any {
	return map[string]any{"messageId": m.run.MessageID, "type": agui.ThinkingTextEnd}
}

func toolStart(evt ai.AssistantMessageEvent) map[string]any {
	toolID, name := toolInfo(evt)
	return map[string]any{"parentMessageId": toolID, "state": "awaiting-input", "toolCallId": toolID, "toolCallName": name, "toolName": name, "type": agui.ToolCallStart}
}

func toolArgs(evt ai.AssistantMessageEvent) map[string]any {
	toolID, _ := toolInfo(evt)
	return map[string]any{"args": evt.Delta, "delta": evt.Delta, "state": "input-streaming", "toolCallId": toolID, "type": agui.ToolCallArgs}
}

func toolEnd(evt ai.AssistantMessageEvent) map[string]any {
	toolID, name := toolInfo(evt)
	var input any
	if evt.ToolCall != nil {
		input = evt.ToolCall.Arguments
	}
	return map[string]any{"input": input, "state": "input-complete", "toolCallId": toolID, "toolCallName": name, "toolName": name, "type": agui.ToolCallEnd}
}

func toolInfo(evt ai.AssistantMessageEvent) (string, string) {
	if evt.ToolCall != nil {
		return firstNonEmpty(evt.ToolCall.ID, fmt.Sprintf("tool-%d", evt.ContentIndex)), firstNonEmpty(evt.ToolCall.Name, "tool")
	}
	return fmt.Sprintf("tool-%d", evt.ContentIndex), "tool"
}

func finalParts(message ai.Message) []map[string]any {
	blocks, ok := message.Content.([]ai.ContentBlock)
	if !ok {
		text := msgconv.AssistantText(message)
		if text == "" {
			return []map[string]any{}
		}
		return []map[string]any{{"content": text, "state": "done", "type": "text"}}
	}
	parts := make([]map[string]any, 0, len(blocks))
	for i, block := range blocks {
		switch block.Type {
		case "text":
			parts = append(parts, map[string]any{"content": block.Text, "state": "done", "type": "text"})
		case "thinking":
			parts = append(parts, map[string]any{"content": block.Thinking, "state": "done", "type": "thinking"})
		case "toolCall", "tool_call":
			id := firstNonEmpty(block.ID, fmt.Sprintf("tool-%d", i))
			parts = append(parts, map[string]any{
				"arguments":  stringifyValue(block.Arguments),
				"id":         id,
				"index":      i,
				"name":       firstNonEmpty(block.Name, "tool"),
				"state":      "input-complete",
				"toolCallId": id,
				"type":       "tool-call",
			})
		}
	}
	if len(parts) == 0 {
		text := msgconv.AssistantText(message)
		if text != "" {
			return []map[string]any{{"content": text, "state": "done", "type": "text"}}
		}
	}
	return parts
}

func runMetadata(run RunInfo, state string, finishReason string, usage ai.Usage, errorMessage string) map[string]any {
	status := map[string]any{"error": nil, "state": state, "terminal": nil}
	if finishReason != "" {
		status["finishReason"] = finishReason
	}
	if errorMessage != "" {
		status["error"] = map[string]any{"message": errorMessage}
	}
	meta := map[string]any{"runId": run.RunID, "status": status, "threadId": run.ThreadID}
	if usageTotal(usage) > 0 {
		meta["usage"] = usageMap(usage)
	}
	return meta
}

func metadata(run RunInfo, state string, finishReason string, usage ai.Usage, preview string, truncated bool, errorMessage string) map[string]any {
	return map[string]any{
		"agent":        map[string]any{"displayName": run.AgentDisplayName, "id": run.AgentID},
		"approvals":    nil,
		"artifacts":    map[string]any{"documents": []any{}, "files": []any{}, "sources": nil},
		"data":         map[string]any{},
		"messageId":    run.MessageID,
		"model":        run.ProviderID + "/" + run.ModelID,
		"preview":      map[string]any{"text": previewText(preview), "truncated": truncated},
		"protocol":     "ag-ui",
		"runId":        run.RunID,
		"schema":       "com.beeper.ai.run.v1",
		"status":       runMetadata(run, state, finishReason, usage, errorMessage)["status"],
		"threadId":     run.ThreadID,
		"usage":        usageMap(usage),
		"usageDetails": map[string]any{},
	}
}

func perMessageProfile(run RunInfo) map[string]any {
	return map[string]any{"displayname": run.AgentDisplayName, "has_fallback": true, "id": run.AgentID}
}

func usageMap(usage ai.Usage) map[string]any {
	return map[string]any{"completionTokens": usage.Output, "promptTokens": usage.Input, "totalTokens": usageTotal(usage)}
}

func usageTotal(usage ai.Usage) int {
	if usage.TotalTokens != 0 {
		return usage.TotalTokens
	}
	return usage.Input + usage.Output
}

func normalizeRunInfo(run RunInfo) RunInfo {
	run.AgentDisplayName = firstNonEmpty(run.AgentDisplayName, run.ModelID, "AI")
	run.AgentID = firstNonEmpty(run.AgentID, run.ModelID, "ai")
	run.MessageID = firstNonEmpty(run.MessageID, "msg-"+run.RunID)
	run.ThreadID = firstNonEmpty(run.ThreadID, run.RunID)
	return run
}

func markdownHTML(markdown string) string {
	if strings.TrimSpace(markdown) == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := goldmark.Convert([]byte(markdown), &buf); err != nil {
		return ""
	}
	return strings.TrimSpace(buf.String())
}

func previewText(text string) string {
	runes := []rune(text)
	if len(runes) <= 240 {
		return text
	}
	return string(runes[:240])
}

func stringifyValue(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}

func errorMessage(evt ai.AssistantMessageEvent) string {
	if evt.Error != nil && evt.Error.ErrorMessage != "" {
		return evt.Error.ErrorMessage
	}
	return firstNonEmpty(string(evt.Reason), "Run failed")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
