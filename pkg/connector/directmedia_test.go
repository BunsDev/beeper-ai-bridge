package connector

import (
	"context"
	"database/sql"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/beeper/ai-bridge/pkg/agent/harness/session"
	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aidb"
	"github.com/beeper/ai-bridge/pkg/aiid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/bridgev2"
	bridgedb "maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
	"maunium.net/go/mautrix/mediaproxy"
)

type fakeMediaUploader struct {
	roomID   id.RoomID
	data     []byte
	fileName string
	mimeType string
}

func (f *fakeMediaUploader) UploadMedia(_ context.Context, roomID id.RoomID, data []byte, fileName, mimeType string) (id.ContentURIString, *event.EncryptedFileInfo, error) {
	f.roomID = roomID
	f.data = append([]byte(nil), data...)
	f.fileName = fileName
	f.mimeType = mimeType
	return id.ContentURIString("mxc://example/image"), nil, nil
}

func TestDirectMediaDownloadsInlineSessionBlock(t *testing.T) {
	ctx := context.Background()
	rawDB, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := dbutil.NewWithDB(rawDB, "sqlite3")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := aidb.NewStore(db, dbutil.ZeroLogger(zerolog.Nop()))
	if err := store.Upgrade(ctx); err != nil {
		t.Fatal(err)
	}
	agentSession, err := store.CreateSession(ctx, session.SQLiteSessionCreateOptions{ID: "session-1"})
	if err != nil {
		t.Fatal(err)
	}
	entryID, err := agentSession.AppendMessage(ctx, ai.Message{
		Role: "assistant",
		Content: []ai.ContentBlock{{
			Type:     "image",
			Data:     "aGVsbG8=",
			MimeType: "text/plain",
		}},
		Timestamp: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	mediaID, err := aiid.MediaIDFor(aiid.MediaMetadata{
		SessionID:    "session-1",
		EntryID:      entryID,
		ContentIndex: 0,
		MimeType:     "text/plain",
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := (&Connector{Store: store}).Download(ctx, mediaID, nil)
	if err != nil {
		t.Fatal(err)
	}
	dataResponse, ok := response.(*mediaproxy.GetMediaResponseData)
	if !ok {
		t.Fatalf("expected data response, got %T", response)
	}
	body, err := io.ReadAll(dataResponse.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello" || dataResponse.ContentType != "text/plain" {
		t.Fatalf("unexpected media response %q %q", body, dataResponse.ContentType)
	}
}

func TestDirectMediaReturnsRetrievalURL(t *testing.T) {
	expiresAt := time.Now().Add(time.Hour).UTC()
	mediaID, err := aiid.MediaIDFor(aiid.MediaMetadata{
		Retrieval: map[string]any{
			"url":        "https://media.example/test.png",
			"expires_at": expiresAt,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := (&Connector{}).Download(context.Background(), mediaID, nil)
	if err != nil {
		t.Fatal(err)
	}
	urlResponse, ok := response.(*mediaproxy.GetMediaResponseURL)
	if !ok {
		t.Fatalf("expected URL response, got %T", response)
	}
	if urlResponse.URL != "https://media.example/test.png" {
		t.Fatalf("unexpected URL response %#v", urlResponse)
	}
}

func TestAssistantImageConvertedMessageUploadsMatrixImage(t *testing.T) {
	const oneByOnePNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII="
	uploader := &fakeMediaUploader{}
	converted, err := assistantImageConvertedMessage(
		context.Background(),
		&bridgev2.Portal{Portal: &bridgedb.Portal{MXID: id.RoomID("!room:example")}},
		uploader,
		ai.ContentBlock{Type: "image", MimeType: "image/png", Data: "data:image/png;base64," + oneByOnePNG, Name: "result.png"},
		aiid.PartID("image-0"),
		&aiid.MessageMetadata{Role: "assistant"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if uploader.roomID != "!room:example" || uploader.fileName != "result.png" || uploader.mimeType != "image/png" || len(uploader.data) == 0 {
		t.Fatalf("unexpected upload %#v", uploader)
	}
	part := converted.Parts[0]
	content := part.Content
	if content.MsgType != event.MsgImage || content.URL != "mxc://example/image" || content.Body != "result.png" {
		t.Fatalf("unexpected converted image content %#v", content)
	}
	if content.Info == nil || content.Info.MimeType != "image/png" || content.Info.Width != 1 || content.Info.Height != 1 {
		t.Fatalf("expected image metadata, got %#v", content.Info)
	}
	if part.DBMetadata.(*aiid.MessageMetadata).Role != "assistant" {
		t.Fatalf("expected assistant metadata, got %#v", part.DBMetadata)
	}
}
