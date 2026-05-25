package harness

import (
	"encoding/json"
	"strings"
	"testing"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func TestCompactionSerialization(t *testing.T) {
	message := agent.AgentMessage{Role: "assistant", Content: []ai.ContentBlock{
		{Type: "thinking", Thinking: "plan"},
		{Type: "text", Text: "done"},
		{Type: "toolCall", Name: "read", Arguments: map[string]any{"path": "/tmp/a"}},
		{Type: "toolCall", Name: "write", Arguments: map[string]any{"path": "/tmp/b"}},
	}}
	serialized := SerializeConversation([]ai.Message{
		{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "hello"}}},
		message,
		{Role: "toolResult", Content: []ai.ContentBlock{{Type: "text", Text: strings.Repeat("x", 2005)}}},
	})
	for _, want := range []string{"[User]: hello", "[Assistant thinking]: plan", "[Assistant]: done", "[Assistant tool calls]: read(path=\"/tmp/a\"); write(path=\"/tmp/b\")", "[... 5 more characters truncated]"} {
		if !strings.Contains(serialized, want) {
			t.Fatalf("expected %q in %q", want, serialized)
		}
	}
}

func TestEstimateContextTokensUsesLastAssistantUsage(t *testing.T) {
	index := 1
	estimate := EstimateContextTokens([]agent.AgentMessage{
		{Role: "user", Content: "before"},
		{Role: "assistant", Usage: ai.Usage{Input: 10, Output: 5, CacheRead: 2}, StopReason: ai.StopReasonStop},
		{Role: "user", Content: "12345678"},
	})
	if estimate.UsageTokens != 17 || estimate.TrailingTokens != 2 || estimate.Tokens != 19 {
		t.Fatalf("unexpected estimate %#v", estimate)
	}
	if estimate.LastUsageIndex == nil || *estimate.LastUsageIndex != index {
		t.Fatalf("unexpected usage index %#v", estimate.LastUsageIndex)
	}
	if ShouldCompact(90, 100, CompactionSettings{Enabled: true, ReserveTokens: 20}) != true {
		t.Fatalf("expected compaction")
	}
	if ShouldCompact(90, 100, CompactionSettings{Enabled: false, ReserveTokens: 20}) != false {
		t.Fatalf("expected disabled compaction")
	}
}

func TestPrepareCompactionSelectsCutPoint(t *testing.T) {
	entries := []json.RawMessage{
		rawEntry(map[string]any{"type": "message", "id": "a", "parentId": nil, "timestamp": "2026-05-23T00:00:00Z", "message": agent.AgentMessage{Role: "user", Content: "start", Timestamp: 1}}),
		rawEntry(map[string]any{"type": "message", "id": "b", "parentId": "a", "timestamp": "2026-05-23T00:00:01Z", "message": agent.AgentMessage{Role: "assistant", Content: []ai.ContentBlock{{Type: "toolCall", Name: "read", Arguments: map[string]any{"path": "/tmp/a"}}}, Timestamp: 2}}),
		rawEntry(map[string]any{"type": "message", "id": "c", "parentId": "b", "timestamp": "2026-05-23T00:00:02Z", "message": agent.AgentMessage{Role: "user", Content: strings.Repeat("x", 40), Timestamp: 3}}),
		rawEntry(map[string]any{"type": "message", "id": "d", "parentId": "c", "timestamp": "2026-05-23T00:00:03Z", "message": agent.AgentMessage{Role: "assistant", Content: []ai.ContentBlock{{Type: "text", Text: "recent"}}, Timestamp: 4}}),
	}
	prep, ok, err := PrepareCompaction(entries, CompactionSettings{Enabled: true, ReserveTokens: 10, KeepRecentTokens: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || prep == nil {
		t.Fatalf("expected preparation")
	}
	if prep.FirstKeptEntryID != "d" {
		t.Fatalf("expected first kept d, got %q", prep.FirstKeptEntryID)
	}
	if !prep.IsSplitTurn {
		t.Fatalf("expected split turn")
	}
	if len(prep.MessagesToSummarize) != 2 || len(prep.TurnPrefixMessages) != 1 {
		t.Fatalf("unexpected messages to summarize %#v prefix %#v", prep.MessagesToSummarize, prep.TurnPrefixMessages)
	}
}

func rawEntry(value map[string]any) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}
