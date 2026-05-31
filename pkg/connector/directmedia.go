package connector

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/beeper/ai-bridge/pkg/agent/harness/session"
	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/mediaproxy"
)

var _ bridgev2.DirectMediableNetwork = (*Connector)(nil)

func (c *Connector) SetUseDirectMedia() {}

func (c *Connector) Download(ctx context.Context, mediaID networkid.MediaID, params map[string]string) (mediaproxy.GetMediaResponse, error) {
	metadata, err := aiid.ParseMediaID(mediaID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse AI media ID: %w", err)
	}
	if response := mediaResponseFromRetrieval(metadata.Retrieval); response != nil {
		return response, nil
	}
	if metadata.SessionID == "" || metadata.EntryID == "" {
		return nil, fmt.Errorf("AI media ID has no session entry")
	}
	if metadata.LoginID == "" {
		return nil, fmt.Errorf("AI media ID has no login ID")
	}
	agentSession, err := c.Store.OpenSession(ctx, networkid.UserLoginID(metadata.LoginID), sessionMetadata(metadata.SessionID))
	if err != nil {
		return nil, err
	}
	raw, err := agentSession.GetEntry(ctx, metadata.EntryID)
	if err != nil {
		return nil, err
	}
	block, err := contentBlockFromEntry(raw, metadata.ContentIndex)
	if err != nil {
		return nil, err
	}
	data, err := decodeContentBlockData(block)
	if err != nil {
		return nil, err
	}
	contentType := metadata.MimeType
	if contentType == "" {
		contentType = block.MimeType
	}
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	return &mediaproxy.GetMediaResponseData{
		Reader:        io.NopCloser(bytes.NewReader(data)),
		ContentType:   contentType,
		ContentLength: int64(len(data)),
	}, nil
}

func mediaResponseFromRetrieval(raw any) mediaproxy.GetMediaResponse {
	if raw == nil {
		return nil
	}
	var retrieval struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	if err := json.Unmarshal(encoded, &retrieval); err != nil || retrieval.URL == "" {
		return nil
	}
	return &mediaproxy.GetMediaResponseURL{URL: retrieval.URL, ExpiresAt: retrieval.ExpiresAt}
}

func sessionMetadata(sessionID string) session.SQLiteSessionMetadata {
	return session.SQLiteSessionMetadata{SessionMetadata: session.SessionMetadata{ID: sessionID}}
}

func contentBlockFromEntry(raw json.RawMessage, contentIndex int) (ai.ContentBlock, error) {
	var entry struct {
		Message json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return ai.ContentBlock{}, err
	}
	var message struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(entry.Message, &message); err != nil {
		return ai.ContentBlock{}, err
	}
	var blocks []ai.ContentBlock
	if err := json.Unmarshal(message.Content, &blocks); err != nil {
		return ai.ContentBlock{}, fmt.Errorf("AI message content has no media blocks: %w", err)
	}
	if contentIndex < 0 || contentIndex >= len(blocks) {
		return ai.ContentBlock{}, fmt.Errorf("content index %d out of range", contentIndex)
	}
	return blocks[contentIndex], nil
}

func decodeContentBlockData(block ai.ContentBlock) ([]byte, error) {
	data, _, err := decodeContentBlockDataWithMIME(block)
	return data, err
}

func decodeContentBlockDataWithMIME(block ai.ContentBlock) ([]byte, string, error) {
	if block.Data == "" {
		return nil, "", fmt.Errorf("AI content block has no inline data")
	}
	data := block.Data
	mimeType := block.MimeType
	if prefix, value, ok := strings.Cut(data, ","); ok && strings.Contains(prefix, ";base64") {
		data = value
		if parsedMime := strings.TrimPrefix(strings.Split(prefix, ";")[0], "data:"); parsedMime != "" && mimeType == "" {
			mimeType = parsedMime
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, "", err
	}
	return decoded, mimeType, nil
}
