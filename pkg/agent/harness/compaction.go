package harness

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

type CompactionSettings struct {
	Enabled          bool
	ReserveTokens    int
	KeepRecentTokens int
}

var DefaultCompactionSettings = CompactionSettings{Enabled: true, ReserveTokens: 16384, KeepRecentTokens: 20000}

type ContextUsageEstimate struct {
	Tokens         int
	UsageTokens    int
	TrailingTokens int
	LastUsageIndex *int
}

type CutPointResult struct {
	FirstKeptEntryIndex int
	TurnStartIndex      int
	IsSplitTurn         bool
}

type CompactionPreparation struct {
	FirstKeptEntryID    string
	MessagesToSummarize []agent.AgentMessage
	TurnPrefixMessages  []agent.AgentMessage
	IsSplitTurn         bool
	TokensBefore        int
	PreviousSummary     string
	Settings            CompactionSettings
}

func SerializeConversation(messages []ai.Message) string {
	parts := []string{}
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			content := textFromContent(msg.Content)
			if content != "" {
				parts = append(parts, "[User]: "+content)
			}
		case "assistant":
			textParts := []string{}
			thinkingParts := []string{}
			toolCalls := []string{}
			for _, block := range contentBlocks(msg.Content) {
				switch block.Type {
				case "text":
					textParts = append(textParts, block.Text)
				case "thinking":
					thinkingParts = append(thinkingParts, block.Thinking)
				case "toolCall":
					toolCalls = append(toolCalls, block.Name+"("+toolArgsString(block.Arguments)+")")
				}
			}
			if len(thinkingParts) > 0 {
				parts = append(parts, "[Assistant thinking]: "+strings.Join(thinkingParts, "\n"))
			}
			if len(textParts) > 0 {
				parts = append(parts, "[Assistant]: "+strings.Join(textParts, "\n"))
			}
			if len(toolCalls) > 0 {
				parts = append(parts, "[Assistant tool calls]: "+strings.Join(toolCalls, "; "))
			}
		case "toolResult":
			content := textFromContent(msg.Content)
			if content != "" {
				parts = append(parts, "[Tool result]: "+truncateForSummary(content, 2000))
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func CalculateContextTokens(usage ai.Usage) int {
	if usage.TotalTokens != 0 {
		return usage.TotalTokens
	}
	return usage.Input + usage.Output + usage.CacheRead + usage.CacheWrite
}

func EstimateContextTokens(messages []agent.AgentMessage) ContextUsageEstimate {
	usage, index, ok := lastAssistantUsageInfo(messages)
	if !ok {
		estimated := 0
		for _, message := range messages {
			estimated += EstimateTokens(message)
		}
		return ContextUsageEstimate{Tokens: estimated, TrailingTokens: estimated}
	}
	usageTokens := CalculateContextTokens(usage)
	trailingTokens := 0
	for i := index + 1; i < len(messages); i++ {
		trailingTokens += EstimateTokens(messages[i])
	}
	return ContextUsageEstimate{Tokens: usageTokens + trailingTokens, UsageTokens: usageTokens, TrailingTokens: trailingTokens, LastUsageIndex: &index}
}

func ShouldCompact(contextTokens int, contextWindow int, settings CompactionSettings) bool {
	return settings.Enabled && contextTokens > contextWindow-settings.ReserveTokens
}

func EstimateTokens(message agent.AgentMessage) int {
	chars := 0
	switch message.Role {
	case "user", "custom", "toolResult":
		chars = contentChars(message.Content)
	case "assistant":
		for _, block := range contentBlocks(message.Content) {
			switch block.Type {
			case "text":
				chars += len(block.Text)
			case "thinking":
				chars += len(block.Thinking)
			case "toolCall":
				chars += len(block.Name) + len(safeJSONString(block.Arguments))
			}
		}
	case "branchSummary", "compactionSummary":
		chars = len(message.Summary)
	}
	return int(math.Ceil(float64(chars) / 4))
}

func FindTurnStartIndex(entries []SessionEntry, entryIndex int, startIndex int) int {
	for i := entryIndex; i >= startIndex; i-- {
		entry := entries[i]
		if entry.Type == "branch_summary" || entry.Type == "custom_message" {
			return i
		}
		if entry.Type == "message" && entry.Message.Role == "user" {
			return i
		}
	}
	return -1
}

func FindCutPoint(entries []SessionEntry, startIndex int, endIndex int, keepRecentTokens int) CutPointResult {
	cutPoints := findValidCutPoints(entries, startIndex, endIndex)
	if len(cutPoints) == 0 {
		return CutPointResult{FirstKeptEntryIndex: startIndex, TurnStartIndex: -1}
	}
	accumulatedTokens := 0
	cutIndex := cutPoints[0]
	for i := endIndex - 1; i >= startIndex; i-- {
		entry := entries[i]
		if entry.Type != "message" {
			continue
		}
		accumulatedTokens += EstimateTokens(entry.Message)
		if accumulatedTokens >= keepRecentTokens {
			for _, cutPoint := range cutPoints {
				if cutPoint >= i {
					cutIndex = cutPoint
					break
				}
			}
			break
		}
	}
	for cutIndex > startIndex {
		prevEntry := entries[cutIndex-1]
		if prevEntry.Type == "compaction" || prevEntry.Type == "message" {
			break
		}
		cutIndex--
	}
	cutEntry := entries[cutIndex]
	isUserMessage := cutEntry.Type == "message" && cutEntry.Message.Role == "user"
	turnStartIndex := -1
	if !isUserMessage {
		turnStartIndex = FindTurnStartIndex(entries, cutIndex, startIndex)
	}
	return CutPointResult{FirstKeptEntryIndex: cutIndex, TurnStartIndex: turnStartIndex, IsSplitTurn: !isUserMessage && turnStartIndex != -1}
}

func PrepareCompaction(rawEntries []json.RawMessage, settings CompactionSettings) (*CompactionPreparation, bool, error) {
	entries, err := ParseSessionEntries(rawEntries)
	if err != nil {
		return nil, false, err
	}
	if len(entries) == 0 || entries[len(entries)-1].Type == "compaction" {
		return nil, false, nil
	}
	prevCompactionIndex := -1
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Type == "compaction" {
			prevCompactionIndex = i
			break
		}
	}
	previousSummary := ""
	boundaryStart := 0
	if prevCompactionIndex >= 0 {
		previousSummary = entries[prevCompactionIndex].Summary
		firstKeptEntryIndex := -1
		for i, entry := range entries {
			if entry.ID == entries[prevCompactionIndex].FirstKeptEntryID {
				firstKeptEntryIndex = i
				break
			}
		}
		if firstKeptEntryIndex >= 0 {
			boundaryStart = firstKeptEntryIndex
		} else {
			boundaryStart = prevCompactionIndex + 1
		}
	}
	context, err := BuildSessionContextFromEntries(entries)
	if err != nil {
		return nil, false, err
	}
	tokensBefore := EstimateContextTokens(context.Messages).Tokens
	cutPoint := FindCutPoint(entries, boundaryStart, len(entries), settings.KeepRecentTokens)
	firstKeptEntry := entries[cutPoint.FirstKeptEntryIndex]
	if firstKeptEntry.ID == "" {
		return nil, false, fmt.Errorf("First kept entry has no UUID - session may need migration")
	}
	historyEnd := cutPoint.FirstKeptEntryIndex
	if cutPoint.IsSplitTurn {
		historyEnd = cutPoint.TurnStartIndex
	}
	messagesToSummarize := []agent.AgentMessage{}
	for i := boundaryStart; i < historyEnd; i++ {
		if msg, ok := messageFromEntryForCompaction(entries[i]); ok {
			messagesToSummarize = append(messagesToSummarize, msg)
		}
	}
	turnPrefixMessages := []agent.AgentMessage{}
	if cutPoint.IsSplitTurn {
		for i := cutPoint.TurnStartIndex; i < cutPoint.FirstKeptEntryIndex; i++ {
			if msg, ok := messageFromEntryForCompaction(entries[i]); ok {
				turnPrefixMessages = append(turnPrefixMessages, msg)
			}
		}
	}
	return &CompactionPreparation{
		FirstKeptEntryID:    firstKeptEntry.ID,
		MessagesToSummarize: messagesToSummarize,
		TurnPrefixMessages:  turnPrefixMessages,
		IsSplitTurn:         cutPoint.IsSplitTurn,
		TokensBefore:        tokensBefore,
		PreviousSummary:     previousSummary,
		Settings:            settings,
	}, true, nil
}

type SessionEntry struct {
	Type             string
	ID               string
	ParentID         *string
	Timestamp        string
	Message          agent.AgentMessage
	Summary          string
	FirstKeptEntryID string
	TokensBefore     int
	FromID           string
	CustomType       string
	Content          any
	Display          bool
	Details          map[string]any
	FromHook         bool
}

func ParseSessionEntries(rawEntries []json.RawMessage) ([]SessionEntry, error) {
	entries := make([]SessionEntry, 0, len(rawEntries))
	for _, raw := range rawEntries {
		entry, err := parseSessionEntry(raw)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func BuildSessionContextFromEntries(entries []SessionEntry) (SessionContextView, error) {
	raw := make([]json.RawMessage, 0, len(entries))
	for _, entry := range entries {
		raw = append(raw, entry.rawJSON())
	}
	return buildSessionContextView(raw)
}

type SessionContextView struct {
	Messages []agent.AgentMessage
}

func parseSessionEntry(raw json.RawMessage) (SessionEntry, error) {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return SessionEntry{}, err
	}
	entry := SessionEntry{Type: stringValue(body["type"]), ID: stringValue(body["id"]), Timestamp: stringValue(body["timestamp"]), Summary: stringValue(body["summary"]), FirstKeptEntryID: stringValue(body["firstKeptEntryId"]), TokensBefore: intValue(body["tokensBefore"]), FromID: stringValue(body["fromId"]), CustomType: stringValue(body["customType"]), Content: body["content"], Display: boolValue(body["display"]), FromHook: boolValue(body["fromHook"])}
	if parent, ok := body["parentId"].(string); ok {
		entry.ParentID = &parent
	}
	if details, ok := body["details"].(map[string]any); ok {
		entry.Details = details
	}
	if msgRaw, ok := body["message"]; ok {
		encoded, _ := json.Marshal(msgRaw)
		_ = json.Unmarshal(encoded, &entry.Message)
	}
	return entry, nil
}

func (e SessionEntry) rawJSON() json.RawMessage {
	body := map[string]any{"type": e.Type, "id": e.ID, "parentId": e.ParentID, "timestamp": e.Timestamp}
	if e.Message.Role != "" {
		body["message"] = e.Message
	}
	if e.Summary != "" {
		body["summary"] = e.Summary
	}
	if e.FirstKeptEntryID != "" {
		body["firstKeptEntryId"] = e.FirstKeptEntryID
	}
	if e.TokensBefore != 0 {
		body["tokensBefore"] = e.TokensBefore
	}
	if e.FromID != "" {
		body["fromId"] = e.FromID
	}
	if e.CustomType != "" {
		body["customType"] = e.CustomType
	}
	if e.Content != nil {
		body["content"] = e.Content
	}
	if e.Display {
		body["display"] = e.Display
	}
	if e.Details != nil {
		body["details"] = e.Details
	}
	if e.FromHook {
		body["fromHook"] = e.FromHook
	}
	raw, _ := json.Marshal(body)
	return raw
}

func buildSessionContextView(rawEntries []json.RawMessage) (SessionContextView, error) {
	type sessionContextBuilder interface {
		BuildSessionContext([]json.RawMessage) (any, error)
	}
	_ = sessionContextBuilder(nil)
	context, err := buildSessionContextLocal(rawEntries)
	if err != nil {
		return SessionContextView{}, err
	}
	return SessionContextView{Messages: context}, nil
}

func buildSessionContextLocal(rawEntries []json.RawMessage) ([]agent.AgentMessage, error) {
	entries, err := ParseSessionEntries(rawEntries)
	if err != nil {
		return nil, err
	}
	messages := []agent.AgentMessage{}
	for _, entry := range entries {
		if msg, ok := messageFromEntry(entry); ok {
			messages = append(messages, msg)
		}
	}
	return messages, nil
}

func findValidCutPoints(entries []SessionEntry, startIndex int, endIndex int) []int {
	cutPoints := []int{}
	for i := startIndex; i < endIndex; i++ {
		entry := entries[i]
		if entry.Type == "message" {
			switch entry.Message.Role {
			case "custom", "branchSummary", "compactionSummary", "user", "assistant":
				cutPoints = append(cutPoints, i)
			}
		}
		if entry.Type == "branch_summary" || entry.Type == "custom_message" {
			cutPoints = append(cutPoints, i)
		}
	}
	return cutPoints
}

func messageFromEntryForCompaction(entry SessionEntry) (agent.AgentMessage, bool) {
	if entry.Type == "compaction" {
		return agent.AgentMessage{}, false
	}
	return messageFromEntry(entry)
}

func messageFromEntry(entry SessionEntry) (agent.AgentMessage, bool) {
	switch entry.Type {
	case "message":
		return entry.Message, true
	case "custom_message":
		return CreateCustomMessage(entry.CustomType, entry.Content, entry.Display, entry.Details, entry.Timestamp), true
	case "branch_summary":
		return CreateBranchSummaryMessage(entry.Summary, entry.FromID, entry.Timestamp), true
	case "compaction":
		return CreateCompactionSummaryMessage(entry.Summary, entry.TokensBefore, entry.Timestamp), true
	default:
		return agent.AgentMessage{}, false
	}
}

func lastAssistantUsageInfo(messages []agent.AgentMessage) (ai.Usage, int, bool) {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role == "assistant" && msg.StopReason != ai.StopReasonAborted && msg.StopReason != ai.StopReasonError && CalculateContextTokens(msg.Usage) > 0 {
			return msg.Usage, i, true
		}
	}
	return ai.Usage{}, -1, false
}

func contentChars(content any) int {
	if text, ok := content.(string); ok {
		return len(text)
	}
	chars := 0
	for _, block := range contentBlocks(content) {
		if block.Type == "text" {
			chars += len(block.Text)
		}
		if block.Type == "image" {
			chars += 4800
		}
	}
	return chars
}

func contentBlocks(content any) []ai.ContentBlock {
	switch typed := content.(type) {
	case []ai.ContentBlock:
		return typed
	case []any:
		blocks := make([]ai.ContentBlock, 0, len(typed))
		for _, item := range typed {
			encoded, _ := json.Marshal(item)
			var block ai.ContentBlock
			if json.Unmarshal(encoded, &block) == nil {
				blocks = append(blocks, block)
			}
		}
		return blocks
	default:
		encoded, _ := json.Marshal(content)
		var blocks []ai.ContentBlock
		_ = json.Unmarshal(encoded, &blocks)
		return blocks
	}
}

func textFromContent(content any) string {
	if text, ok := content.(string); ok {
		return text
	}
	parts := []string{}
	for _, block := range contentBlocks(content) {
		if block.Type == "text" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "")
}

func toolArgsString(args map[string]any) string {
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+safeJSONString(args[key]))
	}
	return strings.Join(parts, ", ")
}

func safeJSONString(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "[unserializable]"
	}
	if string(raw) == "" {
		return "undefined"
	}
	return string(raw)
}

func truncateForSummary(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	truncatedChars := len(text) - maxChars
	return text[:maxChars] + fmt.Sprintf("\n\n[... %d more characters truncated]", truncatedChars)
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func boolValue(value any) bool {
	boolean, _ := value.(bool)
	return boolean
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	default:
		return 0
	}
}
