package msgconv

import (
	"fmt"
	"strings"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"maunium.net/go/mautrix/event"
)

func AssistantText(message ai.Message) string {
	switch content := message.Content.(type) {
	case string:
		return content
	case []ai.ContentBlock:
		return textFromBlocks(content)
	case []any:
		return textFromAnyBlocks(content)
	default:
		if message.ErrorMessage != "" {
			return message.ErrorMessage
		}
		return fmt.Sprint(content)
	}
}

func TextContent(body string) *event.MessageEventContent {
	if body == "" {
		body = " "
	}
	return &event.MessageEventContent{
		MsgType: event.MsgText,
		Body:    body,
	}
}

func NoticeContent(body string) *event.MessageEventContent {
	if body == "" {
		body = " "
	}
	return &event.MessageEventContent{
		MsgType: event.MsgNotice,
		Body:    body,
	}
}

func textFromBlocks(blocks []ai.ContentBlock) string {
	var text strings.Builder
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			text.WriteString(block.Text)
		}
	}
	return text.String()
}

func textFromAnyBlocks(blocks []any) string {
	var text strings.Builder
	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		blockType, _ := block["type"].(string)
		blockText, _ := block["text"].(string)
		if blockType == "text" && blockText != "" {
			text.WriteString(blockText)
		}
	}
	return text.String()
}
