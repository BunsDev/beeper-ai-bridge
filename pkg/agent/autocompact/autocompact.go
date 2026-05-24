package autocompact

import (
	"context"

	"github.com/beeper/ai-bridge/pkg/agent/harness"
	"github.com/beeper/ai-bridge/pkg/agent/harness/session"
	ai "github.com/beeper/ai-bridge/pkg/ai"
	aiutils "github.com/beeper/ai-bridge/pkg/ai/utils"
)

type Reason string

const (
	ReasonOverflow  Reason = "overflow"
	ReasonThreshold Reason = "threshold"
)

type Result struct {
	Reason     Reason
	Compaction harness.CompactResult
	Compacted  bool
}

type Runner struct {
	Harness  *harness.AgentHarness
	Session  *session.Session
	Model    ai.Model
	Settings harness.CompactionSettings
}

func (r Runner) CheckAndCompact(ctx context.Context, assistantMessage ai.Message) (Result, error) {
	reason, ok, err := r.ShouldCompact(ctx, assistantMessage)
	if err != nil || !ok {
		return Result{}, err
	}
	compaction, err := r.Harness.Compact(ctx, "")
	if err != nil {
		return Result{Reason: reason}, err
	}
	return Result{Reason: reason, Compaction: compaction, Compacted: true}, nil
}

func (r Runner) ShouldCompact(ctx context.Context, assistantMessage ai.Message) (Reason, bool, error) {
	if r.Session == nil || assistantMessage.StopReason == ai.StopReasonAborted {
		return "", false, nil
	}
	contextWindow := r.Model.ContextWindow
	if contextWindow == 0 {
		contextWindow = 128000
	}
	settings := r.Settings
	if settings == (harness.CompactionSettings{}) {
		settings = harness.DefaultCompactionSettings
	}
	if aiutils.IsContextOverflow(assistantMessage, contextWindow) {
		return ReasonOverflow, true, nil
	}
	context, err := r.Session.BuildContext(ctx)
	if err != nil {
		return "", false, err
	}
	contextTokens := harness.CalculateContextTokens(assistantMessage.Usage)
	if assistantMessage.StopReason == ai.StopReasonError {
		estimate := harness.EstimateContextTokens(context.Messages)
		if estimate.LastUsageIndex == nil {
			return "", false, nil
		}
		contextTokens = estimate.Tokens
	}
	if harness.ShouldCompact(contextTokens, contextWindow, settings) {
		return ReasonThreshold, true, nil
	}
	return "", false, nil
}
