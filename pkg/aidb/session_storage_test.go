package aidb

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/beeper/ai-bridge/pkg/agent/harness/session"
	ai "github.com/beeper/ai-bridge/pkg/ai"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog"
	"go.mau.fi/util/dbutil"
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
