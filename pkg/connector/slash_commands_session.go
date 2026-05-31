package connector

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/beeper/ai-bridge/pkg/agent/harness"
	"github.com/beeper/ai-bridge/pkg/agent/harness/session"
	"maunium.net/go/mautrix/bridgev2"
)

type sessionCommandStats struct {
	TotalEntries       int
	Messages           int
	UserMessages       int
	AssistantMessages  int
	ToolResultMessages int
	Compactions        int
}

type sessionCommandInfo struct {
	SessionID         string
	CreatedAt         string
	SessionName       string
	LeafID            string
	RoomProvider      string
	RoomModel         string
	RoomReasoning     string
	SessionProvider   string
	SessionModel      string
	SessionReasoning  string
	RequestedModel    string
	SystemPrompt      bool
	SystemPromptChars int
	Responding        bool
	ActiveRunID       string
	ActiveModel       string
	Stats             sessionCommandStats
	ContextMessages   int
	EstimatedTokens   int
	MissingSession    bool
}

func runSessionCommand(cl *Client, ctx context.Context, portal *bridgev2.Portal, roomConfig RoomConfig, _ string, responder aiCommandResponder) error {
	provider, model, canonicalModel, err := cl.resolveCanonicalRoomModel(ctx, roomConfig)
	if err != nil {
		return err
	}
	active := cl.getActiveRun(portal.PortalKey)
	info := sessionCommandInfo{
		RoomProvider:      provider.ID,
		RoomModel:         canonicalModel,
		RoomReasoning:     displayReasoningLevel(cl.reasoningLevelForModel(model, roomConfig)),
		RequestedModel:    roomConfig.ModelID,
		SystemPrompt:      strings.TrimSpace(roomConfig.AdditionalPrompt) != "",
		SystemPromptChars: len([]rune(strings.TrimSpace(roomConfig.AdditionalPrompt))),
		Responding:        active != nil,
	}
	if active != nil {
		info.ActiveRunID = active.runID
		info.ActiveModel = active.provider.ID + "/" + active.model.ID
	}

	meta := portalMetadata(portal)
	if meta.SessionID == "" {
		return responder.Reply(ctx, formatSessionCommandInfo(info))
	}
	info.SessionID = meta.SessionID

	agentSession, err := cl.Main.Store.OpenSession(ctx, session.SQLiteSessionMetadata{
		SessionMetadata: session.SessionMetadata{ID: meta.SessionID},
	})
	if errors.Is(err, sql.ErrNoRows) {
		info.MissingSession = true
		return responder.Reply(ctx, formatSessionCommandInfo(info))
	}
	if err != nil {
		return err
	}
	metadata, err := agentSession.GetMetadata(ctx)
	if err != nil {
		return err
	}
	info.CreatedAt = metadata.CreatedAt
	if name, err := agentSession.GetSessionName(ctx); err == nil && name != nil {
		info.SessionName = *name
	}
	if leafID, err := agentSession.GetLeafID(ctx); err == nil && leafID != nil {
		info.LeafID = *leafID
	}

	branch, err := agentSession.GetBranch(ctx, nil)
	if err != nil {
		return err
	}
	info.Stats, err = sessionCommandStatsFromEntries(branch)
	if err != nil {
		return err
	}
	contextView, err := session.BuildSessionContext(branch)
	if err != nil {
		return err
	}
	info.SessionReasoning = contextView.ThinkingLevel
	if contextView.Model != nil {
		info.SessionProvider = contextView.Model.Provider
		info.SessionModel = contextView.Model.ModelID
	}
	info.ContextMessages = len(contextView.Messages)
	info.EstimatedTokens = harness.EstimateContextTokens(contextView.Messages).Tokens
	return responder.Reply(ctx, formatSessionCommandInfo(info))
}

func sessionCommandStatsFromEntries(entries []json.RawMessage) (sessionCommandStats, error) {
	stats := sessionCommandStats{TotalEntries: len(entries)}
	for _, raw := range entries {
		var entry struct {
			Type    string `json:"type"`
			Message struct {
				Role string `json:"role"`
			} `json:"message"`
		}
		if err := json.Unmarshal(raw, &entry); err != nil {
			return sessionCommandStats{}, err
		}
		switch entry.Type {
		case "message":
			stats.Messages++
			switch entry.Message.Role {
			case "user":
				stats.UserMessages++
			case "assistant":
				stats.AssistantMessages++
			case "toolResult":
				stats.ToolResultMessages++
			}
		case "compaction":
			stats.Compactions++
		}
	}
	return stats, nil
}

func formatSessionCommandInfo(info sessionCommandInfo) string {
	var text strings.Builder
	text.WriteString("AI session:")
	status := "idle"
	if info.Responding {
		status = "responding"
	}
	fmt.Fprintf(&text, "\n- Status: `%s`", status)
	if info.SessionID != "" {
		fmt.Fprintf(&text, "\n- ID: `%s`", info.SessionID)
	}
	if info.CreatedAt != "" {
		fmt.Fprintf(&text, "\n- Created: `%s`", info.CreatedAt)
	}
	if info.SessionName != "" {
		fmt.Fprintf(&text, "\n- Title: `%s`", info.SessionName)
	}
	if info.LeafID != "" {
		fmt.Fprintf(&text, "\n- Leaf entry: `%s`", info.LeafID)
	}
	fmt.Fprintf(&text, "\n- Room provider: `%s`", info.RoomProvider)
	fmt.Fprintf(&text, "\n- Room model: `%s`", info.RoomModel)
	if info.RequestedModel != "" && info.RequestedModel != info.RoomModel {
		fmt.Fprintf(&text, "\n- Requested model: `%s`", info.RequestedModel)
	}
	fmt.Fprintf(&text, "\n- Room reasoning: `%s`", info.RoomReasoning)
	if info.SessionModel != "" {
		fmt.Fprintf(&text, "\n- Last session model: `%s/%s`", info.SessionProvider, info.SessionModel)
	}
	if info.SessionReasoning != "" {
		fmt.Fprintf(&text, "\n- Last session reasoning: `%s`", info.SessionReasoning)
	}
	if info.ActiveRunID != "" {
		fmt.Fprintf(&text, "\n- Active run: `%s`", info.ActiveRunID)
	}
	if info.ActiveModel != "" {
		fmt.Fprintf(&text, "\n- Active model: `%s`", info.ActiveModel)
	}
	systemPrompt := "no"
	if info.SystemPrompt {
		systemPrompt = fmt.Sprintf("yes, %d chars", info.SystemPromptChars)
	}
	fmt.Fprintf(&text, "\n- System prompt: `%s`", systemPrompt)
	if info.SessionID == "" {
		text.WriteString("\n\nNo AI session has been started in this room yet.")
		return text.String()
	}
	if info.MissingSession {
		text.WriteString("\n\nThe room points at a session that no longer exists.")
		return text.String()
	}
	stats := info.Stats
	fmt.Fprintf(&text, "\n- Context messages: `%d`", info.ContextMessages)
	fmt.Fprintf(&text, "\n- Estimated context tokens: `%d`", info.EstimatedTokens)
	fmt.Fprintf(&text, "\n- Messages: `%d` total, `%d` user, `%d` assistant, `%d` tool results", stats.Messages, stats.UserMessages, stats.AssistantMessages, stats.ToolResultMessages)
	fmt.Fprintf(&text, "\n- Compactions: `%d`", stats.Compactions)
	fmt.Fprintf(&text, "\n- Branch entries: `%d`", stats.TotalEntries)
	return text.String()
}
