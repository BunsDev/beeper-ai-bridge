package msgconv

import (
	"fmt"
	"strings"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"maunium.net/go/mautrix/event"
)

func AssistantText(message ai.Message) string {
	var text string
	switch content := message.Content.(type) {
	case string:
		text = content
	case []ai.ContentBlock:
		text = textFromBlocks(content)
	case []any:
		text = textFromAnyBlocks(content)
	default:
		if content != nil {
			text = fmt.Sprint(content)
		}
	}
	if text == "" && message.ErrorMessage != "" {
		return message.ErrorMessage
	}
	return text
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
