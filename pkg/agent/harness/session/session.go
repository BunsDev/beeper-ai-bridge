package session

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	agent "github.com/beeper/ai-bridge/pkg/agent"
)

type SessionMetadata struct {
	ID        string `json:"id"`
	CreatedAt string `json:"createdAt"`
}

type SQLiteSessionMetadata struct {
	SessionMetadata
	Path              string `json:"path"`
	ParentSessionPath string `json:"parentSessionPath,omitempty"`
}

type SessionTreeEntry struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	ParentID  *string         `json:"parentId"`
	Timestamp string          `json:"timestamp"`
	Raw       json.RawMessage `json:"-"`
}

type SessionContext struct {
	Messages      []agent.AgentMessage `json:"messages"`
	ThinkingLevel string               `json:"thinkingLevel"`
	Model         *SessionModel        `json:"model"`
}

const DeletedMessagePlaceholder = "[Deleted message]"

type SessionModel struct {
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
}

type SessionStorage interface {
	GetMetadata(context.Context) (SQLiteSessionMetadata, error)
	GetLeafID(context.Context) (*string, error)
	SetLeafID(context.Context, *string) error
	CreateEntryID(context.Context) (string, error)
	AppendEntry(context.Context, json.RawMessage) (string, error)
	GetEntry(context.Context, string) (json.RawMessage, error)
	FindEntries(context.Context, string) ([]json.RawMessage, error)
	GetLabel(context.Context, string) (*string, error)
	GetEntries(context.Context) ([]json.RawMessage, error)
	GetPathToRoot(context.Context, *string) ([]json.RawMessage, error)
}

type SessionStorageCloser interface {
	Close() error
}

type Session struct {
	storage SessionStorage
}

func NewSession(storage SessionStorage) *Session {
	return &Session{storage: storage}
}

func (s *Session) GetStorage() SessionStorage {
	return s.storage
}

func (s *Session) Close() error {
	closer, ok := s.storage.(SessionStorageCloser)
	if !ok {
		return nil
	}
	return closer.Close()
}

func (s *Session) GetMetadata(ctx context.Context) (SQLiteSessionMetadata, error) {
	return s.storage.GetMetadata(ctx)
}

func (s *Session) GetLeafID(ctx context.Context) (*string, error) {
	return s.storage.GetLeafID(ctx)
}

func (s *Session) GetEntry(ctx context.Context, id string) (json.RawMessage, error) {
	return s.storage.GetEntry(ctx, id)
}

func (s *Session) GetEntries(ctx context.Context) ([]json.RawMessage, error) {
	return s.storage.GetEntries(ctx)
}

func (s *Session) GetBranch(ctx context.Context, fromID *string) ([]json.RawMessage, error) {
	leafID := fromID
	if leafID == nil {
		var err error
		leafID, err = s.storage.GetLeafID(ctx)
		if err != nil {
			return nil, err
		}
	}
	return s.storage.GetPathToRoot(ctx, leafID)
}

func (s *Session) BuildContext(ctx context.Context) (SessionContext, error) {
	branch, err := s.GetBranch(ctx, nil)
	if err != nil {
		return SessionContext{}, err
	}
	return BuildSessionContext(branch)
}

func (s *Session) GetLabel(ctx context.Context, id string) (*string, error) {
	return s.storage.GetLabel(ctx, id)
}

func (s *Session) GetSessionName(ctx context.Context) (*string, error) {
	entries, err := s.storage.FindEntries(ctx, "session_info")
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	var entry map[string]any
	if err := json.Unmarshal(entries[len(entries)-1], &entry); err != nil {
		return nil, err
	}
	name, ok := entry["name"].(string)
	if !ok {
		return nil, nil
	}
	trimmed := trimSpace(name)
	if trimmed == "" {
		return nil, nil
	}
	return &trimmed, nil
}

func (s *Session) AppendMessage(ctx context.Context, message agent.AgentMessage) (string, error) {
	return s.appendTypedEntry(ctx, map[string]any{"type": "message", "message": message})
}

func (s *Session) AppendMessageDeletion(ctx context.Context, targetID string) (string, error) {
	if _, err := s.storage.GetEntry(ctx, targetID); err != nil {
		if errors.Is(err, ErrSessionEntryNotFound) {
			return "", NewSessionError(SessionErrorNotFound, "Entry "+targetID+" not found", nil)
		}
		return "", err
	}
	return s.appendTypedEntry(ctx, map[string]any{"type": "message_delete", "targetId": targetID})
}

func (s *Session) AppendThinkingLevelChange(ctx context.Context, thinkingLevel string) (string, error) {
	return s.appendTypedEntry(ctx, map[string]any{"type": "thinking_level_change", "thinkingLevel": thinkingLevel})
}

func (s *Session) AppendModelChange(ctx context.Context, provider string, modelID string) (string, error) {
	return s.appendTypedEntry(ctx, map[string]any{"type": "model_change", "provider": provider, "modelId": modelID})
}

func (s *Session) AppendCompaction(ctx context.Context, summary string, firstKeptEntryID string, tokensBefore int, details any, fromHook *bool) (string, error) {
	entry := map[string]any{"type": "compaction", "summary": summary, "firstKeptEntryId": firstKeptEntryID, "tokensBefore": tokensBefore}
	if details != nil {
		entry["details"] = details
	}
	if fromHook != nil {
		entry["fromHook"] = *fromHook
	}
	return s.appendTypedEntry(ctx, entry)
}

func (s *Session) AppendCustomEntry(ctx context.Context, customType string, data any) (string, error) {
	entry := map[string]any{"type": "custom", "customType": customType}
	if data != nil {
		entry["data"] = data
	}
	return s.appendTypedEntry(ctx, entry)
}

func (s *Session) AppendCustomMessageEntry(ctx context.Context, customType string, content any, display bool, details any) (string, error) {
	entry := map[string]any{"type": "custom_message", "customType": customType, "content": content, "display": display}
	if details != nil {
		entry["details"] = details
	}
	return s.appendTypedEntry(ctx, entry)
}

func (s *Session) AppendLabel(ctx context.Context, targetID string, label *string) (string, error) {
	if _, err := s.storage.GetEntry(ctx, targetID); err != nil {
		if errors.Is(err, ErrSessionEntryNotFound) {
			return "", NewSessionError(SessionErrorNotFound, "Entry "+targetID+" not found", nil)
		}
		return "", err
	}
	entry := map[string]any{"type": "label", "targetId": targetID}
	if label != nil {
		entry["label"] = *label
	}
	return s.appendTypedEntry(ctx, entry)
}

func (s *Session) AppendSessionName(ctx context.Context, name string) (string, error) {
	return s.appendTypedEntry(ctx, map[string]any{"type": "session_info", "name": trimSpace(name)})
}

type MoveToSummary struct {
	Summary  string
	Details  any
	FromHook *bool
}

func (s *Session) MoveTo(ctx context.Context, entryID *string, summary *MoveToSummary) (*string, error) {
	if entryID != nil {
		if _, err := s.storage.GetEntry(ctx, *entryID); err != nil {
			if errors.Is(err, ErrSessionEntryNotFound) {
				return nil, NewSessionError(SessionErrorNotFound, "Entry "+*entryID+" not found", nil)
			}
			return nil, err
		}
	}
	if err := s.storage.SetLeafID(ctx, entryID); err != nil {
		return nil, err
	}
	if summary == nil {
		return nil, nil
	}
	fromID := "root"
	if entryID != nil {
		fromID = *entryID
	}
	entry := map[string]any{"type": "branch_summary", "parentId": entryID, "fromId": fromID, "summary": summary.Summary}
	if summary.Details != nil {
		entry["details"] = summary.Details
	}
	if summary.FromHook != nil {
		entry["fromHook"] = *summary.FromHook
	}
	id, err := s.appendTypedEntry(ctx, entry)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (s *Session) appendTypedEntry(ctx context.Context, entry map[string]any) (string, error) {
	id, err := s.storage.CreateEntryID(ctx)
	if err != nil {
		return "", err
	}
	leafID, err := s.storage.GetLeafID(ctx)
	if err != nil {
		return "", err
	}
	entry["id"] = id
	entry["parentId"] = leafID
	entry["timestamp"] = CreateTimestamp()
	raw, err := json.Marshal(entry)
	if err != nil {
		return "", err
	}
	return s.storage.AppendEntry(ctx, raw)
}

func BuildSessionContext(pathEntries []json.RawMessage) (SessionContext, error) {
	thinkingLevel := "off"
	var model *SessionModel
	var compaction map[string]any
	deletedMessages := map[string]bool{}

	parsed := make([]map[string]any, 0, len(pathEntries))
	for _, raw := range pathEntries {
		var entry map[string]any
		if err := json.Unmarshal(raw, &entry); err != nil {
			return SessionContext{}, err
		}
		parsed = append(parsed, entry)
		switch entry["type"] {
		case "thinking_level_change":
			if level, ok := entry["thinkingLevel"].(string); ok {
				thinkingLevel = level
			}
		case "model_change":
			provider, providerOK := entry["provider"].(string)
			modelID, modelOK := entry["modelId"].(string)
			if providerOK && modelOK {
				model = &SessionModel{Provider: provider, ModelID: modelID}
			}
		case "message":
			if msg, ok := entry["message"].(map[string]any); ok && msg["role"] == "assistant" {
				provider, providerOK := msg["provider"].(string)
				modelID, modelOK := msg["model"].(string)
				if providerOK && modelOK {
					model = &SessionModel{Provider: provider, ModelID: modelID}
				}
			}
		case "compaction":
			compaction = entry
		case "message_delete":
			if targetID, ok := entry["targetId"].(string); ok && targetID != "" {
				deletedMessages[targetID] = true
			}
		}
	}

	messages := []agent.AgentMessage{}
	appendMessage := func(entry map[string]any) error {
		switch entry["type"] {
		case "message":
			raw, err := json.Marshal(entry["message"])
			if err != nil {
				return err
			}
			var message agent.AgentMessage
			if err := json.Unmarshal(raw, &message); err != nil {
				return err
			}
			if entryID, ok := entry["id"].(string); ok && deletedMessages[entryID] {
				message.Content = DeletedMessagePlaceholder
				message.ToolCallID = ""
				message.ToolName = ""
				message.Details = nil
				message.IsError = false
				message.ErrorMessage = ""
				message.Diagnostics = nil
			}
			messages = append(messages, message)
		case "custom_message":
			messages = append(messages, customMessage(entry))
		case "branch_summary":
			if summary, ok := entry["summary"].(string); ok && summary != "" {
				messages = append(messages, branchSummaryMessage(entry, summary))
			}
		}
		return nil
	}

	if compaction != nil {
		messages = append(messages, compactionSummaryMessage(compaction))
		compactionIndex := -1
		compactionID, _ := compaction["id"].(string)
		for i, entry := range parsed {
			if entry["type"] == "compaction" && entry["id"] == compactionID {
				compactionIndex = i
				break
			}
		}
		firstKeptEntryID, _ := compaction["firstKeptEntryId"].(string)
		foundFirstKept := false
		for i := 0; i < compactionIndex; i++ {
			entry := parsed[i]
			if entry["id"] == firstKeptEntryID {
				foundFirstKept = true
			}
			if foundFirstKept {
				if err := appendMessage(entry); err != nil {
					return SessionContext{}, err
				}
			}
		}
		for i := compactionIndex + 1; i < len(parsed); i++ {
			if err := appendMessage(parsed[i]); err != nil {
				return SessionContext{}, err
			}
		}
	} else {
		for _, entry := range parsed {
			if err := appendMessage(entry); err != nil {
				return SessionContext{}, err
			}
		}
	}

	return SessionContext{Messages: messages, ThinkingLevel: thinkingLevel, Model: model}, nil
}

func customMessage(entry map[string]any) agent.AgentMessage {
	return agent.AgentMessage{
		Role:       "custom",
		Content:    entry["content"],
		Timestamp:  timestampMillis(stringField(entry, "timestamp")),
		CustomType: stringField(entry, "customType"),
		Display:    boolField(entry, "display"),
		Details:    entry["details"],
	}
}

func branchSummaryMessage(entry map[string]any, summary string) agent.AgentMessage {
	return agent.AgentMessage{
		Role:      "branchSummary",
		Summary:   summary,
		FromID:    stringField(entry, "fromId"),
		Timestamp: timestampMillis(stringField(entry, "timestamp")),
	}
}

func compactionSummaryMessage(entry map[string]any) agent.AgentMessage {
	return agent.AgentMessage{
		Role:         "compactionSummary",
		Summary:      stringField(entry, "summary"),
		TokensBefore: intField(entry, "tokensBefore"),
		Timestamp:    timestampMillis(stringField(entry, "timestamp")),
	}
}

func stringField(entry map[string]any, key string) string {
	value, _ := entry[key].(string)
	return value
}

func boolField(entry map[string]any, key string) bool {
	value, _ := entry[key].(bool)
	return value
}

func intField(entry map[string]any, key string) int {
	switch value := entry[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return 0
	}
}

func timestampMillis(value string) int64 {
	if value == "" {
		return 0
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return 0
	}
	return parsed.UnixMilli()
}

func trimSpace(value string) string {
	start := 0
	for start < len(value) && (value[start] == ' ' || value[start] == '\t' || value[start] == '\n' || value[start] == '\r') {
		start++
	}
	end := len(value)
	for end > start && (value[end-1] == ' ' || value[end-1] == '\t' || value[end-1] == '\n' || value[end-1] == '\r') {
		end--
	}
	return value[start:end]
}

var ErrSessionEntryNotFound = NewSessionError(SessionErrorNotFound, "session entry not found", nil)
