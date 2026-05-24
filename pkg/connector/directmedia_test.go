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
	"maunium.net/go/mautrix/mediaproxy"
)

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
