package session

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	agent "github.com/earendil-works/pi-mono/packages/agent/src"
)

func TestSQLiteSessionStorageAppendsEntriesAndBuildsPath(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	storage, err := CreateSQLiteSessionStorage(ctx, dbPath, "/repo", "session-1", "")
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
}

func TestSQLiteSessionErrors(t *testing.T) {
	ctx := context.Background()
	storage, err := CreateSQLiteSessionStorage(ctx, filepath.Join(t.TempDir(), "sessions.db"), "/repo", "", "")
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
	storage, err := CreateSQLiteSessionStorage(ctx, filepath.Join(t.TempDir(), "sessions.db"), "/repo", "", "")
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
	storage, err := CreateSQLiteSessionStorage(ctx, dbPath, "/repo", "session-1", "")
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

	if _, err := session.MoveTo(ctx, &firstID, &MoveToSummary{Summary: "went down branch"}); err != nil {
		t.Fatal(err)
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

	first, err := repo.Create(ctx, SQLiteSessionCreateOptions{ID: "session-1", Cwd: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := first.AppendMessage(ctx, agentMessage("user", "hi", 1)); err != nil {
		t.Fatal(err)
	}
	second, err := repo.Create(ctx, SQLiteSessionCreateOptions{ID: "session-2", Cwd: "/other"})
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
	filtered, err := repo.List(ctx, SQLiteSessionListOptions{Cwd: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].ID != "session-1" {
		t.Fatalf("expected filtered session-1, got %#v", filtered)
	}
	opened, err := repo.Open(ctx, filtered[0])
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
	if err := repo.Delete(ctx, filtered[0]); err != nil {
		t.Fatal(err)
	}
	filtered, err = repo.List(ctx, SQLiteSessionListOptions{Cwd: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 0 {
		t.Fatalf("expected deleted session to be absent, got %#v", filtered)
	}
}

func TestSQLiteSessionRepoForkAtAndBefore(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	repo := NewSQLiteSessionRepo(dbPath)
	source, err := repo.Create(ctx, SQLiteSessionCreateOptions{ID: "source", Cwd: "/repo"})
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
		SQLiteSessionCreateOptions: SQLiteSessionCreateOptions{ID: "fork-at", Cwd: "/repo"},
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
		SQLiteSessionCreateOptions: SQLiteSessionCreateOptions{ID: "fork-before", Cwd: "/repo"},
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
		SQLiteSessionCreateOptions: SQLiteSessionCreateOptions{ID: "fork-invalid", Cwd: "/repo"},
		EntryID:                    assistantID,
		Position:                   "before",
	}); err == nil {
		t.Fatal("expected before fork of non-user message to fail")
	} else if err.Error() != "Entry "+assistantID+" is not a user message" {
		t.Fatalf("unexpected fork error %v", err)
	}
	if _, err := repo.Fork(ctx, sourceMetadata, SQLiteSessionForkOptions{
		SQLiteSessionCreateOptions: SQLiteSessionCreateOptions{ID: "fork-missing", Cwd: "/repo"},
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
