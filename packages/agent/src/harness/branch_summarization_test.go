package harness

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	agent "github.com/earendil-works/pi-mono/packages/agent/src"
	"github.com/earendil-works/pi-mono/packages/agent/src/harness/session"
	ai "github.com/earendil-works/pi-mono/packages/ai/src"
)

func TestCollectEntriesForBranchSummaryUsesCommonAncestor(t *testing.T) {
	ctx := context.Background()
	storage, err := session.CreateSQLiteSessionStorage(ctx, filepath.Join(t.TempDir(), "sessions.db"), "/repo", "session-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	sess := session.NewSession(storage)

	rootID, err := sess.AppendMessage(ctx, agent.AgentMessage{Role: "user", Content: "root", Timestamp: 1})
	if err != nil {
		t.Fatal(err)
	}
	oldID, err := sess.AppendMessage(ctx, agent.AgentMessage{Role: "assistant", Content: []ai.ContentBlock{{Type: "text", Text: "old"}}, Timestamp: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.MoveTo(ctx, &rootID, nil); err != nil {
		t.Fatal(err)
	}
	targetID, err := sess.AppendMessage(ctx, agent.AgentMessage{Role: "assistant", Content: []ai.ContentBlock{{Type: "text", Text: "new"}}, Timestamp: 3})
	if err != nil {
		t.Fatal(err)
	}

	result, err := CollectEntriesForBranchSummary(ctx, sess, &oldID, targetID)
	if err != nil {
		t.Fatal(err)
	}
	if result.CommonAncestorID == nil || *result.CommonAncestorID != rootID {
		t.Fatalf("expected common ancestor root, got %#v", result.CommonAncestorID)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("expected one old-branch entry, got %d", len(result.Entries))
	}
	entry, err := parseSessionEntry(result.Entries[0])
	if err != nil {
		t.Fatal(err)
	}
	if entry.ID != oldID {
		t.Fatalf("expected old branch entry, got %q", entry.ID)
	}
}

func TestPrepareBranchEntriesSkipsToolResultsAndCarriesFileOps(t *testing.T) {
	entries := []json.RawMessage{
		rawEntry(map[string]any{"type": "branch_summary", "id": "s", "parentId": nil, "timestamp": "2026-05-23T00:00:00Z", "summary": "prior", "fromId": "x", "details": map[string]any{"readFiles": []string{"/tmp/read"}, "modifiedFiles": []string{"/tmp/old-edit"}}}),
		rawEntry(map[string]any{"type": "message", "id": "a", "parentId": "s", "timestamp": "2026-05-23T00:00:01Z", "message": agent.AgentMessage{Role: "toolResult", Content: []ai.ContentBlock{{Type: "text", Text: "skip"}}, Timestamp: 1}}),
		rawEntry(map[string]any{"type": "message", "id": "b", "parentId": "a", "timestamp": "2026-05-23T00:00:02Z", "message": agent.AgentMessage{Role: "assistant", Content: []ai.ContentBlock{{Type: "toolCall", Name: "edit", Arguments: map[string]any{"path": "/tmp/new-edit"}}}, Timestamp: 2}}),
	}
	prep, err := PrepareBranchEntries(entries, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(prep.Messages) != 2 {
		t.Fatalf("expected branch summary and assistant, got %#v", prep.Messages)
	}
	if prep.Messages[0].Role != "branchSummary" || prep.Messages[1].Role != "assistant" {
		t.Fatalf("unexpected messages %#v", prep.Messages)
	}
	readFiles, modifiedFiles := ComputeFileLists(prep.FileOps)
	if len(readFiles) != 1 || readFiles[0] != "/tmp/read" {
		t.Fatalf("unexpected read files %#v", readFiles)
	}
	if len(modifiedFiles) != 2 || modifiedFiles[0] != "/tmp/new-edit" || modifiedFiles[1] != "/tmp/old-edit" {
		t.Fatalf("unexpected modified files %#v", modifiedFiles)
	}
}
