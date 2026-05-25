package harness

import (
	"testing"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func TestConvertToLlmHandlesCustomHarnessMessages(t *testing.T) {
	messages := []agent.AgentMessage{
		CreateCustomMessage("note", "hello", true, nil, "2026-05-23T00:00:00Z"),
		CreateBranchSummaryMessage("branch", "a", "2026-05-23T00:00:01Z"),
		CreateCompactionSummaryMessage("compact", 42, "2026-05-23T00:00:02Z"),
		{Role: "user", Content: "plain", Timestamp: 4},
		{Role: "unknown", Content: "drop", Timestamp: 5},
	}

	converted := ConvertToLlm(messages)
	if len(converted) != 4 {
		t.Fatalf("expected 4 converted messages, got %#v", converted)
	}
	assertTextBlock(t, converted[0], "hello")
	assertTextBlock(t, converted[1], BranchSummaryPrefix+"branch"+BranchSummarySuffix)
	assertTextBlock(t, converted[2], CompactionSummaryPrefix+"compact"+CompactionSummarySuffix)
	if converted[3].Role != "user" || converted[3].Content != "plain" {
		t.Fatalf("expected original user message, got %#v", converted[3])
	}
	if converted[0].Timestamp != 1779494400000 {
		t.Fatalf("expected parsed timestamp, got %d", converted[0].Timestamp)
	}
}

func assertTextBlock(t *testing.T, message ai.Message, want string) {
	t.Helper()
	blocks, ok := message.Content.([]ai.ContentBlock)
	if !ok || len(blocks) != 1 {
		t.Fatalf("expected one content block, got %#v", message.Content)
	}
	if blocks[0].Type != "text" || blocks[0].Text != want {
		t.Fatalf("expected text block %q, got %#v", want, blocks[0])
	}
}
