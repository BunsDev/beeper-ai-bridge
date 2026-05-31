package aidb

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/beeper/ai-bridge/pkg/agent/harness/session"
	ai "github.com/beeper/ai-bridge/pkg/ai"
	aistream "github.com/beeper/ai-bridge/pkg/ai-stream"
	"github.com/beeper/ai-bridge/pkg/aiid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/id"
)

func TestBridgeSessionStorageUsesPrefixedTablesAndPreservesEntries(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "bridge.db")
	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	db, err := dbutil.NewWithDB(rawDB, "sqlite3")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := NewStore(db, dbutil.ZeroLogger(zerolog.Nop()))
	if err := store.Upgrade(ctx); err != nil {
		t.Fatal(err)
	}
	agentSession, err := store.CreateSession(ctx, session.SQLiteSessionCreateOptions{ID: "session-1"})
	if err != nil {
		t.Fatal(err)
	}
	userEntryID, err := agentSession.AppendMessage(ctx, sessionTestMessage("user", "hi"))
	if err != nil {
		t.Fatal(err)
	}
	assistantEntryID, err := agentSession.AppendMessage(ctx, sessionTestMessage("assistant", "hello"))
	if err != nil {
		t.Fatal(err)
	}
	opened, err := store.OpenSession(ctx, session.SQLiteSessionMetadata{SessionMetadata: session.SessionMetadata{ID: "session-1"}})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := opened.GetEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if !json.Valid(entries[0]) || !json.Valid(entries[1]) {
		t.Fatalf("expected valid JSON entries %#v", entries)
	}
	if userEntryID == assistantEntryID {
		t.Fatalf("expected distinct entry IDs")
	}
	var aiSessionCount int
	if err := db.QueryRow(ctx, `select count(*) from ai_session`).Scan(&aiSessionCount); err != nil {
		t.Fatal(err)
	}
	if aiSessionCount != 1 {
		t.Fatalf("expected one bridge session, got %d", aiSessionCount)
	}
	rows, err := db.Query(ctx, `select * from ai_session limit 0`)
	if err != nil {
		t.Fatal(err)
	}
	columns, err := rows.Columns()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range columns {
		if column == "cwd" || column == "path" {
			t.Fatalf("bridge session table should not include %s column: %#v", column, columns)
		}
	}
	if _, err := db.Query(ctx, `select count(*) from sessions`); err == nil {
		t.Fatalf("generic sessions table should not exist")
	}
}

func sessionTestMessage(role string, text string) ai.Message {
	return ai.Message{
		Role:      role,
		Content:   text,
		Timestamp: 1,
	}
}

func TestActiveStreamStorageRoundTripsAndDeletes(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "bridge.db")
	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	db, err := dbutil.NewWithDB(rawDB, "sqlite3")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := NewStore(db, dbutil.ZeroLogger(zerolog.Nop()))
	if err := store.Upgrade(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(-10 * time.Minute)
	run := aistream.NewRun("run-1", "session-1", "beeper/gpt-5", "assistant", "AI", now)
	run.MessageID = "assistant:run-1"
	run.Status = aistream.Status{State: "streaming"}
	record := ActiveStreamRecord{
		RunID:      run.RunID,
		LoginID:    networkid.UserLoginID("login"),
		PortalKey:  networkid.PortalKey{ID: networkid.PortalID("!room:example.com"), Receiver: networkid.UserLoginID("login")},
		RoomID:     id.RoomID("!room:example.com"),
		EventID:    id.EventID("$anchor"),
		MessageID:  networkid.MessageID("assistant:run-1"),
		ProviderID: "beeper",
		ModelID:    "gpt-5",
		EntryID:    "entry-1",
		Run:        *run,
		Metadata:   aiid.MessageMetadata{Role: "assistant", RunID: run.RunID, StreamStatus: "streaming"},
		StatusInfo: bridgev2.MessageStatusEventInfo{RoomID: id.RoomID("!room:example.com"), SourceEventID: id.EventID("$source"), TransactionID: "txn"},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := store.UpsertActiveStream(ctx, record); err != nil {
		t.Fatal(err)
	}
	active, err := store.ListActiveStreams(ctx, "login")
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("expected one active stream, got %d", len(active))
	}
	got := active[0]
	if got.RunID != record.RunID || got.Run.RunID != record.RunID || got.Metadata.RunID != record.RunID || got.StatusInfo.SourceEventID != "$source" {
		t.Fatalf("active stream did not round-trip: %#v", got)
	}
	stale, err := store.ListStaleActiveStreams(ctx, "login", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 || stale[0].RunID != record.RunID {
		t.Fatalf("expected stale stream, got %#v", stale)
	}
	if err := store.DeleteActiveStream(ctx, record.RunID); err != nil {
		t.Fatal(err)
	}
	active, err = store.ListActiveStreams(ctx, "login")
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("expected active stream to be deleted, got %#v", active)
	}
}
