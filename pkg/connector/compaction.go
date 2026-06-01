package connector

import (
	"context"
	"errors"
	"fmt"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/id"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	"github.com/beeper/ai-bridge/pkg/agent/autocompact"
	"github.com/beeper/ai-bridge/pkg/agent/harness"
	"github.com/beeper/ai-bridge/pkg/agent/harness/session"
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func (cl *Client) runAutoCompaction(ctx context.Context, publisher bridgev2.BeeperStreamPublisher, roomID id.RoomID, eventID id.EventID, agentHarness *harness.AgentHarness, agentSession *session.Session, model ai.Model, assistantMessage ai.Message) (autocompact.Result, bool) {
	runner := autocompact.Runner{Harness: agentHarness, Session: agentSession, Model: model, Settings: cl.Main.Config.Compaction.Settings()}
	reason, ok, err := runner.ShouldCompact(ctx, assistantMessage)
	if err != nil || !ok {
		return autocompact.Result{}, false
	}
	_ = publisher.Publish(ctx, roomID, eventID, map[string]any{"op": "compaction_start", "reason": reason})
	result, err := runner.CheckAndCompact(ctx, assistantMessage)
	if err != nil {
		_ = publisher.Publish(ctx, roomID, eventID, map[string]any{"op": "compaction_error", "reason": reason, "message": err.Error()})
		return autocompact.Result{Reason: reason}, false
	}
	_ = publisher.Publish(ctx, roomID, eventID, map[string]any{
		"op":                  "compaction_done",
		"reason":              result.Reason,
		"summary":             result.Compaction.Summary,
		"first_kept_entry_id": result.Compaction.FirstKeptEntryID,
		"tokens_before":       result.Compaction.TokensBefore,
	})
	return result, true
}

func runCompactCommand(cl *Client, ctx context.Context, portal *bridgev2.Portal, roomConfig RoomConfig, arg string, responder aiCommandResponder) error {
	if cl.getActiveRun(portal.PortalKey) != nil {
		return fmt.Errorf("an AI response is already running. Use `/abort` to stop it, or wait before compacting")
	}
	if cl.getActiveHarness(portal.PortalKey) != nil {
		return fmt.Errorf("compaction is already running. Use `/abort` to stop it")
	}
	if err := responder.Reply(ctx, "Compacting context..."); err != nil {
		return err
	}
	result, err := cl.compactPortalSession(ctx, portal, roomConfig, arg)
	if err != nil {
		return compactCommandError(err)
	}
	return responder.Reply(ctx, fmt.Sprintf("Compacted context from %d tokens.", result.TokensBefore))
}

func (cl *Client) compactPortalSession(ctx context.Context, portal *bridgev2.Portal, roomConfig RoomConfig, customInstructions string) (harness.CompactResult, error) {
	meta := portalMetadata(portal)
	if meta.SessionID == "" {
		return harness.CompactResult{}, fmt.Errorf("no AI session has been started in this room yet")
	}
	agentSession, err := cl.sessionForPortal(ctx, portal, meta)
	if err != nil {
		return harness.CompactResult{}, err
	}
	provider, modelID, err := cl.resolveProvider(ctx, roomConfig)
	if err != nil {
		return harness.CompactResult{}, err
	}
	model := cl.Main.ModelForProvider(provider, modelID)
	if err := cl.validateReasoningLevel(model, roomConfig); err != nil {
		return harness.CompactResult{}, err
	}
	if err := cl.validateReasoningMode(model, roomConfig); err != nil {
		return harness.CompactResult{}, err
	}
	agentHarness, err := harness.NewAgentHarness(harness.AgentHarnessOptions{
		Session:             agentSession,
		Model:               model,
		ThinkingLevel:       agent.ThinkingLevel(cl.reasoningLevelForModel(model, roomConfig)),
		GetAPIKeyAndHeaders: cl.authForProvider(provider),
		CompactionSettings:  cl.Main.Config.Compaction.Settings(),
	})
	if err != nil {
		return harness.CompactResult{}, err
	}
	cl.setActiveHarness(portal.PortalKey, agentHarness)
	defer cl.clearActiveHarness(portal.PortalKey, agentHarness)
	return agentHarness.Compact(ctx, customInstructions)
}

func compactCommandError(err error) error {
	var compactionErr *harness.CompactionError
	if errors.As(err, &compactionErr) {
		switch compactionErr.Code {
		case harness.CompactionErrorAborted:
			return fmt.Errorf("compaction aborted")
		case harness.CompactionErrorSummarizationFailed:
			return fmt.Errorf("compaction failed while summarizing: %w", err)
		case harness.CompactionErrorInvalidSession:
			return fmt.Errorf("cannot compact this session: %w", err)
		case harness.CompactionErrorNothingToCompact:
			return fmt.Errorf("nothing to compact yet. Send more messages first")
		}
	}
	var harnessErr *harness.AgentHarnessError
	if errors.As(err, &harnessErr) && harnessErr.Code == harness.AgentHarnessErrorBusy {
		return fmt.Errorf("AI session is busy. Use `/abort` to stop the active work, or wait and try again")
	}
	return err
}
