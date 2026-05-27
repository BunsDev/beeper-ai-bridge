package msgconv

import (
	"context"
	"strings"
	"testing"

	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

type fakeMediaDownloader struct {
	data []byte
}

func (f fakeMediaDownloader) DownloadMedia(context.Context, id.ContentURIString, *event.EncryptedFileInfo) ([]byte, error) {
	return f.data, nil
}

func TestFromMatrixUsesMatrixHTMLForFormattedText(t *testing.T) {
	prompt, err := FromMatrix(context.Background(), nil, &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Content: &event.MessageEventContent{
				MsgType:       event.MsgText,
				Body:          "plain text",
				FormattedBody: "<strong>plain text</strong>",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prompt.Text != "<strong>plain text</strong>" {
		t.Fatalf("expected Matrix HTML prompt text, got %q", prompt.Text)
	}
}

func TestFromMatrixIncludesReplySessionContext(t *testing.T) {
	prompt, err := FromMatrix(context.Background(), nil, &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Content: &event.MessageEventContent{MsgType: event.MsgText, Body: "follow up"},
		},
		ReplyTo: &database.Message{Metadata: &aiid.MessageMetadata{
			SessionEntryID: "entry-1",
			Role:           "assistant",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt.Text, "entry-1") || !strings.Contains(prompt.Text, "follow up") {
		t.Fatalf("expected reply context in prompt, got %q", prompt.Text)
	}
}

func TestAudioBlockFromMatrixUsesVoiceMetadata(t *testing.T) {
	block, err := audioBlockFromMatrix(context.Background(), fakeMediaDownloader{data: []byte("voice-data")}, &event.MessageEventContent{
		MsgType:      event.MsgAudio,
		Body:         "voice.ogg",
		URL:          id.ContentURIString("mxc://example/voice"),
		MSC3245Voice: &event.MSC3245Voice{},
		Info: &event.FileInfo{
			MimeType: "audio/ogg",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if block.Type != "audio" || block.MimeType != "audio/ogg" || block.Name != "voice.ogg" {
		t.Fatalf("unexpected audio block metadata: %#v", block)
	}
	if block.Data != "dm9pY2UtZGF0YQ==" {
		t.Fatalf("unexpected audio block data: %q", block.Data)
	}
}

func TestAudioBlockFromMatrixInfersMimeTypeFromFileName(t *testing.T) {
	block, err := audioBlockFromMatrix(context.Background(), fakeMediaDownloader{data: []byte("audio-data")}, &event.MessageEventContent{
		MsgType:  event.MsgAudio,
		Body:     "clip.mp3",
		URL:      id.ContentURIString("mxc://example/clip"),
		FileName: "clip.mp3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if block.MimeType != "audio/mpeg" {
		t.Fatalf("expected inferred mp3 MIME type, got %q", block.MimeType)
	}
}

func TestFromMatrixUsesMediaCaptionInsteadOfFileName(t *testing.T) {
	prompt, err := FromMatrix(context.Background(), fakeMediaDownloader{data: []byte("image-data")}, &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Content: &event.MessageEventContent{
				MsgType:  event.MsgImage,
				Body:     "please inspect this",
				FileName: "photo.png",
				URL:      id.ContentURIString("mxc://example/photo"),
				Info: &event.FileInfo{
					MimeType: "image/png",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prompt.Text != "please inspect this" {
		t.Fatalf("expected image caption, got %q", prompt.Text)
	}
	if len(prompt.Attachments) != 1 || prompt.Attachments[0].Name != "photo.png" {
		t.Fatalf("expected image attachment, got %#v", prompt.Attachments)
	}
}

func TestFromMatrixInlinesTextFile(t *testing.T) {
	prompt, err := FromMatrix(context.Background(), fakeMediaDownloader{data: []byte("hello from file")}, &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Content: &event.MessageEventContent{
				MsgType:  event.MsgFile,
				Body:     "please read this",
				FileName: "notes.md",
				URL:      id.ContentURIString("mxc://example/notes"),
				Info: &event.FileInfo{
					MimeType: "text/markdown",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt.Text, "please read this") || !strings.Contains(prompt.Text, "notes.md") || !strings.Contains(prompt.Text, "hello from file") {
		t.Fatalf("expected file content in prompt, got %q", prompt.Text)
	}
	if len(prompt.Attachments) != 0 {
		t.Fatalf("expected text file to be inlined, got media blocks %#v", prompt.Attachments)
	}
}

func TestFromMatrixInlinesOctetStreamTextFileByExtension(t *testing.T) {
	prompt, err := FromMatrix(context.Background(), fakeMediaDownloader{data: []byte(`{"ok":true}`)}, &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Content: &event.MessageEventContent{
				MsgType:  event.MsgFile,
				FileName: "data.json",
				URL:      id.ContentURIString("mxc://example/data"),
				Info: &event.FileInfo{
					MimeType: "application/octet-stream",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt.Text, `{"ok":true}`) {
		t.Fatalf("expected octet-stream JSON file to be inlined, got %q", prompt.Text)
	}
}

func TestFromMatrixDoesNotTreatFileNameAsCaption(t *testing.T) {
	prompt, err := FromMatrix(context.Background(), fakeMediaDownloader{data: []byte("plain text")}, &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Content: &event.MessageEventContent{
				MsgType: event.MsgFile,
				Body:    "notes.txt",
				URL:     id.ContentURIString("mxc://example/notes"),
				Info: &event.FileInfo{
					MimeType: "text/plain",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(prompt.Text, "notes.txt\n\n") {
		t.Fatalf("did not expect filename body to be treated as caption, got %q", prompt.Text)
	}
}

func TestFromMatrixRejectsBinaryFile(t *testing.T) {
	_, err := FromMatrix(context.Background(), fakeMediaDownloader{data: []byte{0xff, 0xfe}}, &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Content: &event.MessageEventContent{
				MsgType:  event.MsgFile,
				FileName: "archive.zip",
				URL:      id.ContentURIString("mxc://example/archive"),
				Info: &event.FileInfo{
					MimeType: "application/zip",
				},
			},
		},
	})
	if err == nil {
		t.Fatal("expected binary file to be rejected")
	}
}
