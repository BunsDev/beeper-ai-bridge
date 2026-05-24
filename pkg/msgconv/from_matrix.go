package msgconv

import (
	"context"
	"encoding/base64"
	"fmt"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"
)

type MatrixPrompt struct {
	Text   string
	Images []ai.ContentBlock
}

func FromMatrix(ctx context.Context, intent bridgev2.MatrixAPI, msg *bridgev2.MatrixMessage) (MatrixPrompt, error) {
	content := msg.Content
	if content == nil {
		return MatrixPrompt{}, fmt.Errorf("missing message content")
	}
	text := withReplyContext(content.Body, msg)
	switch content.MsgType {
	case "", event.MsgText, event.MsgNotice, event.MsgEmote:
		return MatrixPrompt{Text: text}, nil
	case event.MsgImage:
		block, err := imageBlockFromMatrix(ctx, intent, content)
		if err != nil {
			return MatrixPrompt{}, err
		}
		return MatrixPrompt{Text: withReplyContext(content.GetCaption(), msg), Images: []ai.ContentBlock{block}}, nil
	default:
		return MatrixPrompt{}, fmt.Errorf("unsupported Matrix message type %s", content.MsgType)
	}
}

func withReplyContext(text string, msg *bridgev2.MatrixMessage) string {
	if msg == nil || msg.ReplyTo == nil {
		return text
	}
	meta, ok := msg.ReplyTo.Metadata.(*aiid.MessageMetadata)
	if !ok || meta.SessionEntryID == "" {
		return text
	}
	if meta.Role != "" {
		return fmt.Sprintf("Replying to previous %s message %s:\n\n%s", meta.Role, meta.SessionEntryID, text)
	}
	return fmt.Sprintf("Replying to previous message %s:\n\n%s", meta.SessionEntryID, text)
}

func imageBlockFromMatrix(ctx context.Context, intent bridgev2.MatrixAPI, content *event.MessageEventContent) (ai.ContentBlock, error) {
	if content.URL == "" && content.File == nil {
		return ai.ContentBlock{}, fmt.Errorf("image message has no media")
	}
	data, err := intent.DownloadMedia(ctx, content.URL, content.File)
	if err != nil {
		return ai.ContentBlock{}, fmt.Errorf("failed to download Matrix image: %w", err)
	}
	mimeType := "application/octet-stream"
	if content.Info != nil && content.Info.MimeType != "" {
		mimeType = content.Info.MimeType
	}
	return ai.ContentBlock{
		Type:     "image",
		Data:     base64.StdEncoding.EncodeToString(data),
		MimeType: mimeType,
		Name:     content.GetFileName(),
	}, nil
}
