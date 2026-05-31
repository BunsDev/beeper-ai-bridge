package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	_ "github.com/mattn/go-sqlite3"
)

func TestSQLiteSessionStorageAppendsEntriesAndBuildsPath(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	storage, err := CreateSQLiteSessionStorage(ctx, dbPath, "session-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()

	first := json.RawMessage(`{"type":"message","id":"a","parentId":null,"timestamp":"2026-05-23T00:00:00Z","message":{"role":"user","content":"hi","timestamp":1}}`)
	second := json.RawMessage(`{"type":"message","id":"b","parentId":"a","timestamp":"2026-05-23T00:00:01Z","message":{"role":"assistant","content":[],"timestamp":2}}`)
	if id, err := storage.AppendEntry(ctx, first); err != nil || id != "a" {
		t.Fatalf("append first: id=%q err=%v", id, err)
	}
	if id, err := storage.AppendEntry(ctx, second); err != nil || id != "b" {
		t.Fatalf("append second: id=%q err=%v", id, err)
	}

	leaf, err := storage.GetLeafID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if leaf == nil || *leaf != "b" {
		t.Fatalf("expected leaf b, got %#v", leaf)
	}
	path, err := storage.GetPathToRoot(ctx, leaf)
	if err != nil {
		t.Fatal(err)
	}
	if len(path) != 2 {
		t.Fatalf("expected 2 path entries, got %d", len(path))
	}
	var entry SessionTreeEntry
	if err := json.Unmarshal(path[0], &entry); err != nil {
		t.Fatal(err)
	}
	if entry.ID != "a" {
		t.Fatalf("expected root entry a, got %q", entry.ID)
	}

	rootPath, err := storage.GetPathToRoot(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rootPath == nil || len(rootPath) != 0 {
		t.Fatalf("expected nil leaf path to be empty slice, got %#v", rootPath)
	}
}

func TestSQLiteSessionStoragePreservesRawEntryObjects(t *testing.T) {
	ctx := context.Background()
	storage, err := CreateSQLiteSessionStorage(ctx, filepath.Join(t.TempDir(), "sessions.db"), "session-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()

	root := json.RawMessage(`{"type":"message","id":"raw-root","parentId":null,"timestamp":"2026-05-23T00:00:00.000Z","message":{"role":"user","content":[{"type":"text","text":"hi"},{"type":"toolCall","id":"call_1","name":"read","arguments":{"path":"README.md","flags":["a","b"]}}],"timestamp":1,"provider":"openai","model":"gpt-test","usage":{"inputTokens":3,"outputTokens":4}},"extra":{"nested":{"ok":true},"items":[1,"two",null]}}`)
	child := json.RawMessage(`{"type":"label","id":"raw-child","parentId":"raw-root","timestamp":"2026-05-23T00:00:01.000Z","targetId":"raw-root","label":"  Root Label  ","extra":{"kept":true}}`)
	if _, err := storage.AppendEntry(ctx, root); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.AppendEntry(ctx, child); err != nil {
		t.Fatal(err)
	}

	gotRoot, err := storage.GetEntry(ctx, "raw-root")
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, root, gotRoot)
	gotChild, err := storage.GetEntry(ctx, "raw-child")
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, child, gotChild)
	all, err := storage.GetEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected two entries, got %d", len(all))
	}
	assertJSONEqual(t, root, all[0])
	assertJSONEqual(t, child, all[1])
	path, err := storage.GetPathToRoot(ctx, ptr("raw-child"))
	if err != nil {
		t.Fatal(err)
	}
	if len(path) != 2 {
		t.Fatalf("expected two path entries, got %d", len(path))
	}
	assertJSONEqual(t, root, path[0])
	assertJSONEqual(t, child, path[1])
}

func TestSQLiteSessionStorageUsesDBUtilVersionTable(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	storage, err := CreateSQLiteSessionStorage(ctx, dbPath, "session-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	var compat int
	if err := db.QueryRow(`select version, compat from session_version`).Scan(&version, &compat); err != nil {
		t.Fatal(err)
	}
	if version != 1 || compat != 1 {
		t.Fatalf("expected session schema version 1/1, got %d/%d", version, compat)
	}
}

func TestSQLiteSessionErrors(t *testing.T) {
	ctx := context.Background()
	storage, err := CreateSQLiteSessionStorage(ctx, filepath.Join(t.TempDir(), "sessions.db"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	if err := storage.SetLeafID(ctx, ptr("missing")); err == nil {
		t.Fatalf("expected missing entry error")
	} else {
		var sessionErr *SessionError
		if !errors.As(err, &sessionErr) || sessionErr.Code != SessionErrorNotFound {
			t.Fatalf("unexpected error %#v", err)
		}
		if err.Error() != "Entry missing not found" {
			t.Fatalf("unexpected missing leaf error %v", err)
		}
	}
	if _, err := storage.AppendEntry(ctx, []byte(`{"type":"message"}`)); err == nil {
		t.Fatalf("expected invalid entry error")
	} else {
		var sessionErr *SessionError
		if !errors.As(err, &sessionErr) || sessionErr.Code != SessionErrorInvalidEntry {
			t.Fatalf("unexpected error %#v", err)
		}
	}
	if _, err := storage.AppendEntry(ctx, []byte(`{"type":"message","id":"child","parentId":"missing-parent","timestamp":"2026-05-23T00:00:00Z","message":{"role":"user","content":"hi","timestamp":1}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.GetPathToRoot(ctx, ptr("child")); err == nil {
		t.Fatalf("expected invalid session error for missing parent")
	} else {
		var sessionErr *SessionError
		if !errors.As(err, &sessionErr) || sessionErr.Code != SessionErrorInvalidSession {
			t.Fatalf("unexpected error %#v", err)
		}
		if err.Error() != "Entry missing-parent not found" {
			t.Fatalf("unexpected missing parent error %v", err)
		}
	}
	raw, err := GetEntryMaybe(ctx, storage, "missing")
	if err != nil || raw != nil {
		t.Fatalf("expected missing entry to map to nil, got raw=%#v err=%v", raw, err)
	}
}

func TestUUIDv7ShapeAndSQLiteGeneratedIDs(t *testing.T) {
	id := UUIDv7()
	if len(id) != 36 || id[14] != '7' || id[19] != '8' && id[19] != '9' && id[19] != 'a' && id[19] != 'b' {
		t.Fatalf("expected uuidv7 shape, got %q", id)
	}
	if strings.Count(id, "-") != 4 {
		t.Fatalf("expected dashed UUID, got %q", id)
	}
	ctx := context.Background()
	storage, err := CreateSQLiteSessionStorage(ctx, filepath.Join(t.TempDir(), "sessions.db"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	metadata, err := storage.GetMetadata(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata.ID) != 36 || metadata.ID[14] != '7' {
		t.Fatalf("expected generated session ID to be UUIDv7, got %q", metadata.ID)
	}
	entryID, err := storage.CreateEntryID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entryID) != 8 || uuidTimePrefix(entryID) == 0 {
		t.Fatalf("expected generated entry ID prefix, got %q", entryID)
	}
}

func TestSessionLabelsNamesMoveAndContext(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	storage, err := CreateSQLiteSessionStorage(ctx, dbPath, "session-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	session := NewSession(storage)

	firstID, err := session.AppendMessage(ctx, agentMessage("user", "hi", 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.AppendThinkingLevelChange(ctx, "high"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.AppendModelChange(ctx, "openai", "gpt-test"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.AppendSessionName(ctx, "  demo  "); err != nil {
		t.Fatal(err)
	}
	label := "Start"
	if _, err := session.AppendLabel(ctx, firstID, &label); err != nil {
		t.Fatal(err)
	}
	if _, err := session.AppendLabel(ctx, "missing", &label); err == nil {
		t.Fatalf("expected missing label target error")
	} else if err.Error() != "Entry missing not found" {
		t.Fatalf("unexpected missing label target error %v", err)
	}
	gotLabel, err := session.GetLabel(ctx, firstID)
	if err != nil {
		t.Fatal(err)
	}
	if gotLabel == nil || *gotLabel != "Start" {
		t.Fatalf("expected label Start, got %#v", gotLabel)
	}
	name, err := session.GetSessionName(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if name == nil || *name != "demo" {
		t.Fatalf("expected session name demo, got %#v", name)
	}
	if _, err := session.AppendSessionName(ctx, "  "); err != nil {
		t.Fatal(err)
	}
	name, err = session.GetSessionName(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if name != nil {
		t.Fatalf("expected blank latest session name to clear name, got %#v", name)
	}
	context, err := session.BuildContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if context.ThinkingLevel != "high" {
		t.Fatalf("expected thinking high, got %q", context.ThinkingLevel)
	}
	if context.Model == nil || context.Model.Provider != "openai" || context.Model.ModelID != "gpt-test" {
		t.Fatalf("unexpected model %#v", context.Model)
	}
	if len(context.Messages) != 1 || context.Messages[0].Content != "hi" {
		t.Fatalf("unexpected messages %#v", context.Messages)
	}
	if _, err := session.AppendMessageDeletion(ctx, firstID); err != nil {
		t.Fatal(err)
	}
	context, err = session.BuildContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(context.Messages) != 1 || context.Messages[0].Content != DeletedMessagePlaceholder {
		t.Fatalf("expected deleted placeholder, got %#v", context.Messages)
	}
	if _, err := session.AppendMessageDeletion(ctx, "missing"); err == nil {
		t.Fatalf("expected missing deletion target error")
	} else if err.Error() != "Entry missing not found" {
		t.Fatalf("unexpected missing deletion target error %v", err)
	}

	if _, err := session.MoveTo(ctx, &firstID, &MoveToSummary{Summary: "went down branch"}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.MoveTo(ctx, ptr("missing"), nil); err == nil {
		t.Fatalf("expected missing move target error")
	} else if err.Error() != "Entry missing not found" {
		t.Fatalf("unexpected missing move target error %v", err)
	}
	context, err = session.BuildContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(context.Messages) != 2 {
		t.Fatalf("expected message plus branch summary, got %#v", context.Messages)
	}
	if context.Messages[1].Role != "branchSummary" || context.Messages[1].Summary != "went down branch" {
		t.Fatalf("unexpected branch summary %#v", context.Messages[1])
	}
}

func TestBuildSessionContextUsesLatestCompaction(t *testing.T) {
	entries := []json.RawMessage{
		json.RawMessage(`{"type":"message","id":"a","parentId":null,"timestamp":"2026-05-23T00:00:00Z","message":{"role":"user","content":"drop","timestamp":1}}`),
		json.RawMessage(`{"type":"message","id":"b","parentId":"a","timestamp":"2026-05-23T00:00:01Z","message":{"role":"user","content":"keep","timestamp":2}}`),
		json.RawMessage(`{"type":"compaction","id":"c","parentId":"b","timestamp":"2026-05-23T00:00:02Z","summary":"summary","firstKeptEntryId":"b","tokensBefore":12}`),
		json.RawMessage(`{"type":"message","id":"d","parentId":"c","timestamp":"2026-05-23T00:00:03Z","message":{"role":"assistant","content":[],"timestamp":3,"provider":"openai","model":"gpt-test"}}`),
	}
	context, err := BuildSessionContext(entries)
	if err != nil {
		t.Fatal(err)
	}
	if len(context.Messages) != 3 {
		t.Fatalf("expected summary, kept message, assistant; got %#v", context.Messages)
	}
	if context.Messages[0].Role != "compactionSummary" || context.Messages[0].Summary != "summary" || context.Messages[0].TokensBefore != 12 {
		t.Fatalf("unexpected compaction summary %#v", context.Messages[0])
	}
	if context.Messages[1].Content != "keep" {
		t.Fatalf("expected kept message, got %#v", context.Messages[1])
	}
	if context.Model == nil || context.Model.Provider != "openai" || context.Model.ModelID != "gpt-test" {
		t.Fatalf("unexpected model %#v", context.Model)
	}
}

func TestSQLiteSessionRepoCreateListOpenDelete(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	var repo SessionRepo = NewSQLiteSessionRepo(dbPath)

	first, err := repo.Create(ctx, SQLiteSessionCreateOptions{ID: "session-1"})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := first.AppendMessage(ctx, agentMessage("user", "hi", 1)); err != nil {
		t.Fatal(err)
	}
	second, err := repo.Create(ctx, SQLiteSessionCreateOptions{ID: "session-2"})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := second.AppendMessage(ctx, agentMessage("user", "other", 2)); err != nil {
		t.Fatal(err)
	}

	all, err := repo.List(ctx, SQLiteSessionListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected two sessions, got %#v", all)
	}
	var firstMetadata *SQLiteSessionMetadata
	for i := range all {
		if all[i].ID == "session-1" {
			firstMetadata = &all[i]
			break
		}
	}
	if firstMetadata == nil {
		t.Fatalf("expected session-1 in list, got %#v", all)
	}
	opened, err := repo.Open(ctx, *firstMetadata)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	entries, err := opened.GetEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected opened session entry, got %#v", entries)
	}
	if err := repo.Delete(ctx, *firstMetadata); err != nil {
		t.Fatal(err)
	}
	remaining, err := repo.List(ctx, SQLiteSessionListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].ID != "session-2" {
		t.Fatalf("expected only session-2 to remain, got %#v", remaining)
	}
}

func TestSQLiteSessionRepoForkAtAndBefore(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	repo := NewSQLiteSessionRepo(dbPath)
	source, err := repo.Create(ctx, SQLiteSessionCreateOptions{ID: "source"})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	rootID, err := source.AppendMessage(ctx, agentMessage("user", "root", 1))
	if err != nil {
		t.Fatal(err)
	}
	assistantID, err := source.AppendMessage(ctx, agentMessage("assistant", []any{}, 2))
	if err != nil {
		t.Fatal(err)
	}
	nextID, err := source.AppendMessage(ctx, agentMessage("user", "next", 3))
	if err != nil {
		t.Fatal(err)
	}
	sourceMetadata, err := source.GetMetadata(ctx)
	if err != nil {
		t.Fatal(err)
	}

	atFork, err := repo.Fork(ctx, sourceMetadata, SQLiteSessionForkOptions{
		SQLiteSessionCreateOptions: SQLiteSessionCreateOptions{ID: "fork-at"},
		EntryID:                    assistantID,
		Position:                   "at",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer atFork.Close()
	atEntries, err := atFork.GetBranch(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(atEntries) != 2 {
		t.Fatalf("expected fork-at root+assistant, got %#v", atEntries)
	}

	beforeFork, err := repo.Fork(ctx, sourceMetadata, SQLiteSessionForkOptions{
		SQLiteSessionCreateOptions: SQLiteSessionCreateOptions{ID: "fork-before"},
		EntryID:                    nextID,
		Position:                   "before",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer beforeFork.Close()
	beforeEntries, err := beforeFork.GetBranch(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(beforeEntries) != 2 {
		t.Fatalf("expected fork-before root+assistant, got %#v", beforeEntries)
	}
	var first SessionTreeEntry
	if err := json.Unmarshal(beforeEntries[0], &first); err != nil {
		t.Fatal(err)
	}
	if first.ID != rootID {
		t.Fatalf("expected root entry %q, got %#v", rootID, first)
	}
	if _, err := repo.Fork(ctx, sourceMetadata, SQLiteSessionForkOptions{
		SQLiteSessionCreateOptions: SQLiteSessionCreateOptions{ID: "fork-invalid"},
		EntryID:                    assistantID,
		Position:                   "before",
	}); err == nil {
		t.Fatal("expected before fork of non-user message to fail")
	} else if err.Error() != "Entry "+assistantID+" is not a user message" {
		t.Fatalf("unexpected fork error %v", err)
	}
	label, err := source.AppendLabel(ctx, rootID, ptr("root"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Fork(ctx, sourceMetadata, SQLiteSessionForkOptions{
		SQLiteSessionCreateOptions: SQLiteSessionCreateOptions{ID: "fork-label"},
		EntryID:                    label,
		Position:                   "before",
	}); err == nil {
		t.Fatal("expected before fork of label entry to fail")
	} else if err.Error() != "Entry "+label+" is not a user message" {
		t.Fatalf("unexpected fork error %v", err)
	}
	if _, err := repo.Fork(ctx, sourceMetadata, SQLiteSessionForkOptions{
		SQLiteSessionCreateOptions: SQLiteSessionCreateOptions{ID: "fork-missing"},
		EntryID:                    "missing",
		Position:                   "at",
	}); err == nil {
		t.Fatal("expected missing fork target to fail")
	} else {
		var sessionErr *SessionError
		if !errors.As(err, &sessionErr) || sessionErr.Code != SessionErrorInvalidForkTarget {
			t.Fatalf("expected invalid_fork_target, got %#v", err)
		}
	}
	if _, err := repo.Open(ctx, SQLiteSessionMetadata{SessionMetadata: SessionMetadata{ID: "missing"}, Path: dbPath}); err == nil {
		t.Fatal("expected missing session open to fail")
	} else {
		var sessionErr *SessionError
		if !errors.As(err, &sessionErr) || sessionErr.Code != SessionErrorNotFound {
			t.Fatalf("expected not_found, got %#v", err)
		}
	}
}

func agentMessage(role string, content any, timestamp int64) agent.AgentMessage {
	return agent.AgentMessage{Role: role, Content: content, Timestamp: timestamp}
}

func ptr(value string) *string {
	return &value
}

func assertJSONEqual(t *testing.T, want json.RawMessage, got json.RawMessage) {
	t.Helper()
	var wantValue any
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("invalid expected json: %v", err)
	}
	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("invalid actual json: %v", err)
	}
	if !reflect.DeepEqual(wantValue, gotValue) {
		t.Fatalf("json mismatch\nwant: %s\n got: %s", want, got)
	}
}
