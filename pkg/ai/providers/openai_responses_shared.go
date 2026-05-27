package providers

import (
	"encoding/json"
	"fmt"
	"strings"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	aiutils "github.com/beeper/ai-bridge/pkg/ai/utils"
)

type OpenAIResponsesStreamOptions struct {
	ServiceTier             string
	ResolveServiceTier      func(responseServiceTier string, requestServiceTier string) string
	ApplyServiceTierPricing func(usage *ai.Usage, serviceTier string)
}

type ConvertResponsesMessagesOptions struct {
	IncludeSystemPrompt *bool
}

type ConvertResponsesToolsOptions struct {
	Strict     *bool
	StrictNull bool
}

func ConvertResponsesMessages(model ai.Model, llmContext ai.Context, options ...ConvertResponsesMessagesOptions) []map[string]any {
	input := []map[string]any{}
	includeSystemPrompt := true
	if len(options) > 0 && options[0].IncludeSystemPrompt != nil {
		includeSystemPrompt = *options[0].IncludeSystemPrompt
	}
	if includeSystemPrompt && llmContext.SystemPrompt != "" {
		role := "system"
		if model.Reasoning {
			role = "developer"
		}
		input = append(input, map[string]any{"role": role, "content": aiutils.SanitizeSurrogates(llmContext.SystemPrompt)})
	}
	transformedMessages := transformMessages(llmContext.Messages, model, normalizeResponsesToolCallID)
	for msgIndex, msg := range transformedMessages {
		switch msg.Role {
		case "user":
			content := responsesUserContent(msg.Content)
			if _, isString := msg.Content.(string); !isString && isEmptyResponsesContent(content) {
				continue
			}
			input = append(input, map[string]any{"role": "user", "content": content})
		case "assistant":
			for _, block := range contentBlocks(msg.Content) {
				switch block.Type {
				case "thinking":
					if block.ThinkingSignature != "" {
						var reasoning map[string]any
						if json.Unmarshal([]byte(block.ThinkingSignature), &reasoning) == nil {
							if isReplayableResponsesReasoningItem(reasoning) {
								input = append(input, reasoning)
							}
						}
					}
				case "text":
					item := map[string]any{
						"type":    "message",
						"role":    "assistant",
						"content": []map[string]any{{"type": "output_text", "text": aiutils.SanitizeSurrogates(block.Text), "annotations": []any{}}},
						"status":  "completed",
						"id":      responsesTextID(block, msgIndex),
					}
					if phase := responsesTextPhase(block); phase != "" {
						item["phase"] = phase
					}
					input = append(input, item)
				case "toolCall":
					callID, itemID := splitResponsesToolCallID(block.ID)
					item := map[string]any{"type": "function_call", "call_id": callID, "name": block.Name, "arguments": mustJSON(block.Arguments)}
					if itemID != "" && !shouldDropResponsesToolItemID(msg, model, itemID) {
						item["id"] = itemID
					}
					input = append(input, item)
				}
			}
		case "toolResult":
			callID := strings.Split(msg.ToolCallID, "|")[0]
			input = append(input, map[string]any{"type": "function_call_output", "call_id": callID, "output": responsesToolOutput(model, msg.Content)})
		}
	}
	return input
}

func isEmptyResponsesContent(content any) bool {
	switch typed := content.(type) {
	case []map[string]any:
		return len(typed) == 0
	case []any:
		return len(typed) == 0
	default:
		return false
	}
}

func ConvertResponsesTools(tools []ai.Tool, options ...ConvertResponsesToolsOptions) []map[string]any {
	strict := false
	var strictValue any = strict
	if len(options) > 0 && options[0].Strict != nil {
		strict = *options[0].Strict
		strictValue = strict
	}
	if len(options) > 0 && options[0].StrictNull {
		strictValue = nil
	}
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		out = append(out, map[string]any{"type": "function", "name": tool.Name, "description": tool.Description, "parameters": tool.Parameters, "strict": strictValue})
	}
	return out
}

func normalizeResponsesToolCallID(id string, model ai.Model, source ai.Message) string {
	normalizeIDPart := func(part string) string {
		normalized := invalidToolCallIDChar.ReplaceAllString(part, "_")
		normalized = truncateString(normalized, 64)
		return strings.TrimRight(normalized, "_")
	}
	if !responsesToolCallProvider(model.Provider) {
		return normalizeIDPart(id)
	}
	if !strings.Contains(id, "|") {
		return normalizeIDPart(id)
	}
	parts := strings.SplitN(id, "|", 2)
	callID := normalizeIDPart(parts[0])
	itemID := normalizeIDPart(parts[1])
	if source.Provider != model.Provider || source.API != model.API {
		itemID = truncateString("fc_"+shortHash(parts[1]), 64)
	}
	if !strings.HasPrefix(itemID, "fc_") {
		itemID = normalizeIDPart("fc_" + itemID)
	}
	return callID + "|" + itemID
}

func responsesToolCallProvider(provider ai.Provider) bool {
	return provider == "openai" || provider == "openai-codex" || provider == "opencode"
}

func responsesUserContent(content any) any {
	if text, ok := content.(string); ok {
		return []map[string]any{{"type": "input_text", "text": aiutils.SanitizeSurrogates(text)}}
	}
	parts := []map[string]any{}
	for _, block := range contentBlocks(content) {
		switch block.Type {
		case "text":
			parts = append(parts, map[string]any{"type": "input_text", "text": aiutils.SanitizeSurrogates(block.Text)})
		case "image":
			parts = append(parts, map[string]any{"type": "input_image", "detail": "auto", "image_url": "data:" + block.MimeType + ";base64," + block.Data})
		case "audio":
			part, _ := inputAudioPart(block)
			parts = append(parts, part)
		}
	}
	return parts
}

func responsesToolOutput(model ai.Model, content any) any {
	text := toolResultText(content)
	hasText := text != ""
	hasImages := false
	parts := []map[string]any{}
	if hasText {
		parts = append(parts, map[string]any{"type": "input_text", "text": text})
	}
	for _, block := range contentBlocks(content) {
		if block.Type == "image" {
			hasImages = true
			if modelSupportsImage(model) {
				parts = append(parts, map[string]any{"type": "input_image", "detail": "auto", "image_url": "data:" + block.MimeType + ";base64," + block.Data})
			}
		}
	}
	if hasImages && modelSupportsImage(model) {
		return parts
	}
	if hasText {
		return aiutils.SanitizeSurrogates(text)
	}
	return "(see attached image)"
}

func responsesTextID(block ai.ContentBlock, msgIndex int) string {
	parsed := parseResponsesTextSignature(block.TextSignature)
	if parsed.ID == "" {
		return fmt.Sprintf("msg_%d", msgIndex)
	}
	if len(parsed.ID) > 64 {
		return "msg_" + shortHash(parsed.ID)
	}
	return parsed.ID
}

func responsesTextPhase(block ai.ContentBlock) string {
	return parseResponsesTextSignature(block.TextSignature).Phase
}

type responsesTextSignature struct {
	ID    string
	Phase string
}

func parseResponsesTextSignature(signature string) responsesTextSignature {
	if signature == "" {
		return responsesTextSignature{}
	}
	if strings.HasPrefix(signature, "{") {
		var parsed struct {
			V     int    `json:"v"`
			ID    string `json:"id"`
			Phase string `json:"phase"`
		}
		if json.Unmarshal([]byte(signature), &parsed) == nil && parsed.V == 1 && parsed.ID != "" {
			out := responsesTextSignature{ID: parsed.ID}
			if parsed.Phase == "commentary" || parsed.Phase == "final_answer" {
				out.Phase = parsed.Phase
			}
			return out
		}
	}
	return responsesTextSignature{ID: signature}
}

func shouldDropResponsesToolItemID(msg ai.Message, model ai.Model, itemID string) bool {
	return msg.Model != model.ID && msg.Provider == model.Provider && msg.API == model.API && strings.HasPrefix(itemID, "fc_")
}

func isReplayableResponsesReasoningItem(item map[string]any) bool {
	if itemType, _ := item["type"].(string); itemType != "reasoning" {
		return true
	}
	if encryptedContent, ok := item["encrypted_content"].(string); ok && encryptedContent != "" {
		return true
	}
	id, _ := item["id"].(string)
	return !strings.HasPrefix(id, "rs_")
}

func splitResponsesToolCallID(id string) (string, string) {
	parts := strings.SplitN(id, "|", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}
