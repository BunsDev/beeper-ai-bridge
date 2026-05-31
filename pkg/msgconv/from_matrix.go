package msgconv

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
)

type MatrixPrompt struct {
	Text        string
	Attachments []ai.ContentBlock
}

const maxTextFileChars = 20000

type matrixMediaDownloader interface {
	DownloadMedia(ctx context.Context, uri id.ContentURIString, file *event.EncryptedFileInfo) ([]byte, error)
}

func FromMatrix(ctx context.Context, intent matrixMediaDownloader, msg *bridgev2.MatrixMessage) (MatrixPrompt, error) {
	content := msg.Content
	if content == nil {
		return MatrixPrompt{}, fmt.Errorf("missing message content")
	}
	text := withReplyContext(matrixPromptText(content), msg)
	switch content.MsgType {
	case "", event.MsgText, event.MsgNotice, event.MsgEmote:
		return MatrixPrompt{Text: text}, nil
	case event.MsgLocation:
		return MatrixPrompt{Text: withReplyContext(matrixLocationText(content), msg)}, nil
	case event.MsgImage:
		block, err := imageBlockFromMatrix(ctx, intent, content)
		if err != nil {
			return MatrixPrompt{}, err
		}
		return MatrixPrompt{Text: withReplyContext(matrixCaptionText(content), msg), Attachments: []ai.ContentBlock{block}}, nil
	case event.MsgAudio:
		block, err := audioBlockFromMatrix(ctx, intent, content)
		if err != nil {
			return MatrixPrompt{}, err
		}
		return MatrixPrompt{Text: withReplyContext(matrixCaptionText(content), msg), Attachments: []ai.ContentBlock{block}}, nil
	case event.MsgFile:
		fileText, err := textFileFromMatrix(ctx, intent, content)
		if err != nil {
			return MatrixPrompt{}, err
		}
		return MatrixPrompt{Text: appendPromptAttachment(withReplyContext(matrixCaptionText(content), msg), fileText)}, nil
	default:
		return MatrixPrompt{}, fmt.Errorf("unsupported Matrix message type %s", content.MsgType)
	}
}

func matrixPromptText(content *event.MessageEventContent) string {
	if content.FormattedBody != "" {
		return content.FormattedBody
	}
	return content.Body
}

func matrixCaptionText(content *event.MessageEventContent) string {
	plain := content.GetCaption()
	if plain == "" {
		return ""
	}
	if formatted := content.GetFormattedCaption(); formatted != "" {
		return formatted
	}
	return plain
}

func matrixLocationText(content *event.MessageEventContent) string {
	geoURI := strings.TrimSpace(content.GeoURI)
	body := strings.TrimSpace(matrixPromptText(content))
	switch {
	case geoURI != "" && body != "" && body != geoURI:
		return fmt.Sprintf("Location: %s\n%s", body, geoURI)
	case geoURI != "":
		return "Location: " + geoURI
	case body != "":
		return "Location: " + body
	default:
		return "Location"
	}
}

func appendPromptAttachment(text, attachment string) string {
	if strings.TrimSpace(text) == "" {
		return attachment
	}
	return text + "\n\n" + attachment
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

func imageBlockFromMatrix(ctx context.Context, intent matrixMediaDownloader, content *event.MessageEventContent) (ai.ContentBlock, error) {
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
	} else if extMime := mime.TypeByExtension(filepath.Ext(content.GetFileName())); extMime != "" {
		mimeType = extMime
	}
	return ai.ContentBlock{
		Type:     "image",
		Data:     base64.StdEncoding.EncodeToString(data),
		MimeType: mimeType,
		Name:     content.GetFileName(),
	}, nil
}

func audioBlockFromMatrix(ctx context.Context, intent matrixMediaDownloader, content *event.MessageEventContent) (ai.ContentBlock, error) {
	if content.URL == "" && content.File == nil {
		return ai.ContentBlock{}, fmt.Errorf("audio message has no media")
	}
	data, err := intent.DownloadMedia(ctx, content.URL, content.File)
	if err != nil {
		return ai.ContentBlock{}, fmt.Errorf("failed to download Matrix audio: %w", err)
	}
	mimeType := "application/octet-stream"
	if content.Info != nil && content.Info.MimeType != "" {
		mimeType = content.Info.MimeType
	} else if extMime := mime.TypeByExtension(filepath.Ext(content.GetFileName())); extMime != "" {
		mimeType = extMime
	}
	return ai.ContentBlock{
		Type:     "audio",
		Data:     base64.StdEncoding.EncodeToString(data),
		MimeType: mimeType,
		Name:     content.GetFileName(),
	}, nil
}

func textFileFromMatrix(ctx context.Context, intent matrixMediaDownloader, content *event.MessageEventContent) (string, error) {
	if content.URL == "" && content.File == nil {
		return "", fmt.Errorf("file message has no media")
	}
	mimeType := fileMimeType(content)
	fileName := content.GetFileName()
	if !isTextLikeFile(mimeType, fileName) {
		return "", fmt.Errorf("unsupported file MIME type %s", mimeType)
	}
	data, err := intent.DownloadMedia(ctx, content.URL, content.File)
	if err != nil {
		return "", fmt.Errorf("failed to download Matrix file: %w", err)
	}
	if len(data) >= 3 && data[0] == 0xef && data[1] == 0xbb && data[2] == 0xbf {
		data = data[3:]
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("text file %s is not valid UTF-8", fileName)
	}
	text := string(data)
	truncated := false
	runes := []rune(text)
	if len(runes) > maxTextFileChars {
		text = string(runes[:maxTextFileChars])
		truncated = true
	}
	if mimeType == "" {
		mimeType = "text/plain"
	}
	return formatTextFileAttachment(fileName, mimeType, text, truncated), nil
}

func fileMimeType(content *event.MessageEventContent) string {
	if content.Info != nil {
		return strings.ToLower(strings.TrimSpace(content.Info.MimeType))
	}
	return ""
}

func isTextLikeFile(mimeType, fileName string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	if strings.HasPrefix(mimeType, "text/") {
		return true
	}
	switch mimeType {
	case "application/json",
		"application/csv",
		"application/ld+json",
		"application/manifest+json",
		"application/x-ndjson",
		"application/xml",
		"application/xhtml+xml",
		"application/yaml",
		"application/x-yaml",
		"application/toml",
		"application/javascript",
		"application/ecmascript",
		"application/sql",
		"application/x-subrip":
		return true
	case "", "application/octet-stream":
		return isTextLikeExtension(fileName)
	default:
		return strings.HasSuffix(mimeType, "+json") || strings.HasSuffix(mimeType, "+xml")
	}
}

func isTextLikeExtension(fileName string) bool {
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".txt", ".md", ".markdown", ".json", ".jsonl", ".ndjson", ".xml", ".yaml", ".yml", ".toml",
		".csv", ".tsv", ".log", ".ini", ".env", ".conf", ".cfg", ".go", ".js", ".jsx", ".ts", ".tsx",
		".py", ".rb", ".rs", ".java", ".c", ".h", ".cpp", ".hpp", ".cs", ".php", ".sh", ".bash",
		".zsh", ".fish", ".sql", ".html", ".htm", ".css", ".scss", ".sass", ".less":
		return true
	default:
		return false
	}
}

func formatTextFileAttachment(fileName, mimeType, text string, truncated bool) string {
	fileName = strings.NewReplacer("\n", " ", "\r", " ").Replace(fileName)
	if fileName == "" {
		fileName = "attachment"
	}
	header := fmt.Sprintf("Attached file %q (%s):", fileName, mimeType)
	if truncated {
		header += fmt.Sprintf(" first %d characters shown", maxTextFileChars)
	}
	return fmt.Sprintf("%s\n\n%s", header, text)
}
