package msgconv

import (
	"context"
	"strings"
	"testing"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/beeper/ai-bridge/pkg/aiid"
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

func TestFromMatrixUsesFormattedMediaCaption(t *testing.T) {
	prompt, err := FromMatrix(context.Background(), fakeMediaDownloader{data: []byte("image-data")}, &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Content: &event.MessageEventContent{
				MsgType:       event.MsgImage,
				Body:          "please inspect this",
				Format:        event.FormatHTML,
				FormattedBody: "please <strong>inspect</strong> this",
				FileName:      "photo.png",
				URL:           id.ContentURIString("mxc://example/photo"),
				Info: &event.FileInfo{
					MimeType: "image/png",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prompt.Text != "please <strong>inspect</strong> this" {
		t.Fatalf("expected formatted image caption, got %q", prompt.Text)
	}
}

func TestFromMatrixDoesNotTreatFormattedFileNameAsCaption(t *testing.T) {
	prompt, err := FromMatrix(context.Background(), fakeMediaDownloader{data: []byte("image-data")}, &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Content: &event.MessageEventContent{
				MsgType:       event.MsgImage,
				Body:          "photo.png",
				Format:        event.FormatHTML,
				FormattedBody: "<strong>photo.png</strong>",
				FileName:      "photo.png",
				URL:           id.ContentURIString("mxc://example/photo"),
				Info: &event.FileInfo{
					MimeType: "image/png",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prompt.Text != "" {
		t.Fatalf("did not expect filename formatted body to be treated as caption, got %q", prompt.Text)
	}
}

func TestFromMatrixUsesFormattedFileCaption(t *testing.T) {
	prompt, err := FromMatrix(context.Background(), fakeMediaDownloader{data: []byte("BEGIN:VCALENDAR\nEND:VCALENDAR")}, &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Content: &event.MessageEventContent{
				MsgType:       event.MsgFile,
				Body:          "please read this",
				Format:        event.FormatHTML,
				FormattedBody: "please <em>read</em> this",
				FileName:      "invite.ics",
				URL:           id.ContentURIString("mxc://example/invite"),
				Info: &event.FileInfo{
					MimeType: "text/calendar",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt.Text, "please <em>read</em> this") || !strings.Contains(prompt.Text, "invite.ics") {
		t.Fatalf("expected formatted file caption and attachment, got %q", prompt.Text)
	}
}

func TestFromMatrixConvertsLocationToText(t *testing.T) {
	prompt, err := FromMatrix(context.Background(), nil, &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Content: &event.MessageEventContent{
				MsgType: event.MsgLocation,
				Body:    "Office",
				GeoURI:  "geo:52.3676,4.9041",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prompt.Text != "Location: Office\ngeo:52.3676,4.9041" {
		t.Fatalf("expected location prompt text, got %q", prompt.Text)
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

func TestFromMatrixInlinesAdditionalTextLikeMIMETypes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mimeType string
		fileName string
		data     string
	}{
		{name: "csv", mimeType: "application/csv", fileName: "data.csv", data: "a,b\n1,2"},
		{name: "subrip", mimeType: "application/x-subrip", fileName: "captions.srt", data: "1\n00:00:00,000 --> 00:00:01,000\nHello"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prompt, err := FromMatrix(context.Background(), fakeMediaDownloader{data: []byte(tc.data)}, &bridgev2.MatrixMessage{
				MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
					Content: &event.MessageEventContent{
						MsgType:  event.MsgFile,
						FileName: tc.fileName,
						URL:      id.ContentURIString("mxc://example/file"),
						Info: &event.FileInfo{
							MimeType: tc.mimeType,
						},
					},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(prompt.Text, tc.fileName) || !strings.Contains(prompt.Text, tc.data) {
				t.Fatalf("expected %s file to be inlined, got %q", tc.mimeType, prompt.Text)
			}
		})
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
