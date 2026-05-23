package compaction

import (
	"context"
	"encoding/json"

	agent "github.com/earendil-works/pi-mono/packages/agent/src"
	harness "github.com/earendil-works/pi-mono/packages/agent/src/harness"
	"github.com/earendil-works/pi-mono/packages/agent/src/harness/session"
)

type BranchSummaryDetails = harness.BranchSummaryDetails
type BranchPreparation = harness.BranchPreparation
type CollectEntriesResult = harness.CollectEntriesResult

func CollectEntriesForBranchSummary(ctx context.Context, session *session.Session, oldLeafID *string, targetID string) (CollectEntriesResult, error) {
	return harness.CollectEntriesForBranchSummary(ctx, session, oldLeafID, targetID)
}

func PrepareBranchEntries(rawEntries []json.RawMessage, tokenBudget int) (BranchPreparation, error) {
	return harness.PrepareBranchEntries(rawEntries, tokenBudget)
}

func MessagesFromBranchPreparation(preparation BranchPreparation) []agent.AgentMessage {
	return append([]agent.AgentMessage{}, preparation.Messages...)
}
