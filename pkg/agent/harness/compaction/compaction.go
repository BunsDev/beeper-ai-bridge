package compaction

import (
	"encoding/json"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	harness "github.com/beeper/ai-bridge/pkg/agent/harness"
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

type FileOperations = harness.FileOperations
type CompactionDetails = harness.CompactionDetails
type CompactionSettings = harness.CompactionSettings
type ContextUsageEstimate = harness.ContextUsageEstimate
type CutPointResult = harness.CutPointResult
type CompactionPreparation = harness.CompactionPreparation
type SessionEntry = harness.SessionEntry
type SessionContextView = harness.SessionContextView

var DefaultCompactionSettings = harness.DefaultCompactionSettings

func CalculateContextTokens(usage ai.Usage) int {
	return harness.CalculateContextTokens(usage)
}

func EstimateContextTokens(messages []agent.AgentMessage) ContextUsageEstimate {
	return harness.EstimateContextTokens(messages)
}

func ShouldCompact(contextTokens int, contextWindow int, settings CompactionSettings) bool {
	return harness.ShouldCompact(contextTokens, contextWindow, settings)
}

func EstimateTokens(message agent.AgentMessage) int {
	return harness.EstimateTokens(message)
}

func FindTurnStartIndex(entries []SessionEntry, entryIndex int, startIndex int) int {
	return harness.FindTurnStartIndex(entries, entryIndex, startIndex)
}

func FindCutPoint(entries []SessionEntry, startIndex int, endIndex int, keepRecentTokens int) CutPointResult {
	return harness.FindCutPoint(entries, startIndex, endIndex, keepRecentTokens)
}

func PrepareCompaction(rawEntries []json.RawMessage, settings CompactionSettings) (*CompactionPreparation, bool, error) {
	return harness.PrepareCompaction(rawEntries, settings)
}

func ParseSessionEntries(rawEntries []json.RawMessage) ([]SessionEntry, error) {
	return harness.ParseSessionEntries(rawEntries)
}

func BuildSessionContextFromEntries(entries []SessionEntry) (SessionContextView, error) {
	return harness.BuildSessionContextFromEntries(entries)
}
