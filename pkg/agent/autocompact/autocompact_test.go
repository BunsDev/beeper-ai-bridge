package autocompact

import (
	"context"
	"path/filepath"
	"testing"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	"github.com/beeper/ai-bridge/pkg/agent/harness"
	"github.com/beeper/ai-bridge/pkg/agent/harness/session"
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func TestShouldCompactOnThreshold(t *testing.T) {
	ctx := context.Background()
	agentSession := testSession(t, ctx)
	assistant := agent.AgentMessage{
		Role:       "assistant",
		Content:    []ai.ContentBlock{{Type: "text", Text: "ok"}},
		Usage:      ai.Usage{Input: 90, Output: 5},
		StopReason: ai.StopReasonStop,
		Timestamp:  2,
	}
	if _, err := agentSession.AppendMessage(ctx, assistant); err != nil {
		t.Fatal(err)
	}
	reason, ok, err := Runner{
		Session:  agentSession,
		Model:    ai.Model{ContextWindow: 100},
		Settings: harness.CompactionSettings{Enabled: true, ReserveTokens: 10, KeepRecentTokens: 1},
	}.ShouldCompact(ctx, ai.Message(assistant))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || reason != ReasonThreshold {
		t.Fatalf("expected threshold compaction, got %q %v", reason, ok)
	}
}

func TestShouldCompactOnOverflow(t *testing.T) {
	ctx := context.Background()
	agentSession := testSession(t, ctx)
	message := ai.Message{Role: "assistant", StopReason: ai.StopReasonError, ErrorMessage: "input is too long for requested model"}
	reason, ok, err := Runner{Session: agentSession, Model: ai.Model{ContextWindow: 100}}.ShouldCompact(ctx, message)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || reason != ReasonOverflow {
		t.Fatalf("expected overflow compaction, got %q %v", reason, ok)
	}
}

func testSession(t *testing.T, ctx context.Context) *session.Session {
	t.Helper()
	storage, err := session.CreateSQLiteSessionStorage(ctx, filepath.Join(t.TempDir(), "sessions.db"), "session-1", "")
	if err != nil {
		t.Fatal(err)
	}
	return session.NewSession(storage)
}
