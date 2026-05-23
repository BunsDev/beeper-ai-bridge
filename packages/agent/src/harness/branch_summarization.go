package harness

import (
	"context"
	"encoding/json"

	agent "github.com/earendil-works/pi-mono/packages/agent/src"
	"github.com/earendil-works/pi-mono/packages/agent/src/harness/session"
)

type BranchSummaryDetails struct {
	ReadFiles     []string `json:"readFiles"`
	ModifiedFiles []string `json:"modifiedFiles"`
}

type BranchPreparation struct {
	Messages    []agent.AgentMessage
	FileOps     FileOperations
	TotalTokens int
}

type CollectEntriesResult struct {
	Entries          []json.RawMessage
	CommonAncestorID *string
}

func CollectEntriesForBranchSummary(ctx context.Context, session *session.Session, oldLeafID *string, targetID string) (CollectEntriesResult, error) {
	if oldLeafID == nil {
		return CollectEntriesResult{}, nil
	}
	oldPath, err := session.GetBranch(ctx, oldLeafID)
	if err != nil {
		return CollectEntriesResult{}, err
	}
	oldIDs := map[string]bool{}
	for _, raw := range oldPath {
		entry, err := parseSessionEntry(raw)
		if err != nil {
			return CollectEntriesResult{}, err
		}
		oldIDs[entry.ID] = true
	}
	targetPath, err := session.GetBranch(ctx, &targetID)
	if err != nil {
		return CollectEntriesResult{}, err
	}
	var commonAncestorID *string
	for i := len(targetPath) - 1; i >= 0; i-- {
		entry, err := parseSessionEntry(targetPath[i])
		if err != nil {
			return CollectEntriesResult{}, err
		}
		if oldIDs[entry.ID] {
			id := entry.ID
			commonAncestorID = &id
			break
		}
	}
	entries := []json.RawMessage{}
	current := *oldLeafID
	for commonAncestorID == nil || current != *commonAncestorID {
		raw, err := session.GetEntry(ctx, current)
		if err != nil {
			return CollectEntriesResult{}, err
		}
		entry, err := parseSessionEntry(raw)
		if err != nil {
			return CollectEntriesResult{}, err
		}
		entries = append(entries, raw)
		if entry.ParentID == nil {
			break
		}
		current = *entry.ParentID
	}
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return CollectEntriesResult{Entries: entries, CommonAncestorID: commonAncestorID}, nil
}

func PrepareBranchEntries(rawEntries []json.RawMessage, tokenBudget int) (BranchPreparation, error) {
	entries, err := ParseSessionEntries(rawEntries)
	if err != nil {
		return BranchPreparation{}, err
	}
	messages := []agent.AgentMessage{}
	fileOps := CreateFileOps()
	totalTokens := 0
	for _, entry := range entries {
		if entry.Type == "branch_summary" && !entry.FromHook && entry.Details != nil {
			for _, path := range stringSliceFromAny(entry.Details["readFiles"]) {
				fileOps.Read[path] = true
			}
			for _, path := range stringSliceFromAny(entry.Details["modifiedFiles"]) {
				fileOps.Edited[path] = true
			}
		}
	}
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		message, ok := messageFromEntryForBranch(entry)
		if !ok {
			continue
		}
		ExtractFileOpsFromMessage(message, fileOps)
		tokens := EstimateTokens(message)
		if tokenBudget > 0 && totalTokens+tokens > tokenBudget {
			if entry.Type == "compaction" || entry.Type == "branch_summary" {
				if float64(totalTokens) < float64(tokenBudget)*0.9 {
					messages = append([]agent.AgentMessage{message}, messages...)
					totalTokens += tokens
				}
			}
			break
		}
		messages = append([]agent.AgentMessage{message}, messages...)
		totalTokens += tokens
	}
	return BranchPreparation{Messages: messages, FileOps: fileOps, TotalTokens: totalTokens}, nil
}

func messageFromEntryForBranch(entry SessionEntry) (agent.AgentMessage, bool) {
	if entry.Type == "message" && entry.Message.Role == "toolResult" {
		return agent.AgentMessage{}, false
	}
	return messageFromEntry(entry)
}
