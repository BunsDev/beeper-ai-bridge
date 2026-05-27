package providers

import (
	"strings"
	"time"

	ai "github.com/beeper/ai-bridge/pkg/ai"
)

const nonVisionUserImagePlaceholder = "(image omitted: model does not support images)"
const nonVisionToolImagePlaceholder = "(tool image omitted: model does not support images)"
const nonAudioUserAudioPlaceholder = "(audio omitted: model does not support audio input)"
const nonAudioToolAudioPlaceholder = "(tool audio omitted: model does not support audio input)"

func TransformMessages(messages []ai.Message, model ai.Model, normalizeToolCallID func(string, ai.Model, ai.Message) string) []ai.Message {
	return transformMessages(messages, model, normalizeToolCallID)
}

func transformMessages(messages []ai.Message, model ai.Model, normalizeToolCallID func(string, ai.Model, ai.Message) string) []ai.Message {
	toolCallIDMap := map[string]string{}
	transformed := make([]ai.Message, 0, len(messages))
	for _, msg := range messages {
		msg = downgradeUnsupportedImages(msg, model)
		msg = downgradeUnsupportedAudio(msg, model)
		switch msg.Role {
		case "toolResult":
			if normalized := toolCallIDMap[msg.ToolCallID]; normalized != "" && normalized != msg.ToolCallID {
				msg.ToolCallID = normalized
			}
			transformed = append(transformed, msg)
		case "assistant":
			isSameModel := msg.Provider == model.Provider && msg.API == model.API && msg.Model == model.ID
			blocks := []ai.ContentBlock{}
			for _, block := range contentBlocks(msg.Content) {
				switch block.Type {
				case "thinking":
					if block.Redacted {
						if isSameModel {
							blocks = append(blocks, block)
						}
						continue
					}
					if isSameModel && block.ThinkingSignature != "" {
						blocks = append(blocks, block)
						continue
					}
					if strings.TrimSpace(block.Thinking) == "" {
						continue
					}
					if isSameModel {
						blocks = append(blocks, block)
					} else {
						blocks = append(blocks, ai.ContentBlock{Type: "text", Text: block.Thinking})
					}
				case "text":
					if isSameModel {
						blocks = append(blocks, block)
					} else {
						blocks = append(blocks, ai.ContentBlock{Type: "text", Text: block.Text})
					}
				case "toolCall":
					if !isSameModel {
						block.ThoughtSignature = ""
						if normalizeToolCallID != nil {
							normalized := normalizeToolCallID(block.ID, model, msg)
							if normalized != block.ID {
								toolCallIDMap[block.ID] = normalized
								block.ID = normalized
							}
						}
					}
					blocks = append(blocks, block)
				default:
					blocks = append(blocks, block)
				}
			}
			msg.Content = blocks
			transformed = append(transformed, msg)
		default:
			transformed = append(transformed, msg)
		}
	}

	result := []ai.Message{}
	pendingToolCalls := []ai.ContentBlock{}
	existingToolResultIDs := map[string]bool{}
	insertSyntheticToolResults := func() {
		for _, toolCall := range pendingToolCalls {
			if !existingToolResultIDs[toolCall.ID] {
				result = append(result, ai.Message{
					Role:       "toolResult",
					ToolCallID: toolCall.ID,
					ToolName:   toolCall.Name,
					Content:    []ai.ContentBlock{{Type: "text", Text: "No result provided"}},
					IsError:    true,
					Timestamp:  time.Now().UnixMilli(),
				})
			}
		}
		pendingToolCalls = nil
		existingToolResultIDs = map[string]bool{}
	}
	for _, msg := range transformed {
		switch msg.Role {
		case "assistant":
			insertSyntheticToolResults()
			if msg.StopReason == ai.StopReasonError || msg.StopReason == ai.StopReasonAborted {
				continue
			}
			result = append(result, msg)
			for _, block := range contentBlocks(msg.Content) {
				if block.Type == "toolCall" {
					pendingToolCalls = append(pendingToolCalls, block)
				}
			}
		case "toolResult":
			existingToolResultIDs[msg.ToolCallID] = true
			result = append(result, msg)
		case "user":
			insertSyntheticToolResults()
			result = append(result, msg)
		default:
			result = append(result, msg)
		}
	}
	insertSyntheticToolResults()
	return result
}

func downgradeUnsupportedAudio(msg ai.Message, model ai.Model) ai.Message {
	if modelSupportsAudio(model) {
		return msg
	}
	if msg.Role == "user" {
		if _, ok := msg.Content.(string); !ok {
			msg.Content = replaceBlocksWithPlaceholder(contentBlocks(msg.Content), "audio", nonAudioUserAudioPlaceholder)
		}
	}
	if msg.Role == "toolResult" {
		msg.Content = replaceBlocksWithPlaceholder(contentBlocks(msg.Content), "audio", nonAudioToolAudioPlaceholder)
	}
	return msg
}

func downgradeUnsupportedImages(msg ai.Message, model ai.Model) ai.Message {
	if modelSupportsImage(model) {
		return msg
	}
	if msg.Role == "user" {
		if _, ok := msg.Content.(string); !ok {
			msg.Content = replaceBlocksWithPlaceholder(contentBlocks(msg.Content), "image", nonVisionUserImagePlaceholder)
		}
	}
	if msg.Role == "toolResult" {
		msg.Content = replaceBlocksWithPlaceholder(contentBlocks(msg.Content), "image", nonVisionToolImagePlaceholder)
	}
	return msg
}

func replaceBlocksWithPlaceholder(content []ai.ContentBlock, blockType string, placeholder string) []ai.ContentBlock {
	result := []ai.ContentBlock{}
	previousWasPlaceholder := false
	for _, block := range content {
		if block.Type == blockType {
			if !previousWasPlaceholder {
				result = append(result, ai.ContentBlock{Type: "text", Text: placeholder})
			}
			previousWasPlaceholder = true
			continue
		}
		result = append(result, block)
		previousWasPlaceholder = block.Type == "text" && block.Text == placeholder
	}
	return result
}
