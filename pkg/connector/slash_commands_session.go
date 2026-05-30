package connector

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
	SessionID      string
	CreatedAt      string
	Model          string
	Reasoning      string
	SystemPrompt   bool
	Responding     bool
	Stats          sessionCommandStats
	MissingSession bool
}

func runSessionCommand(cl *Client, ctx context.Context, portal *bridgev2.Portal, roomConfig RoomConfig, _ string, responder aiCommandResponder) error {
	_, model, canonicalModel, err := cl.resolveCanonicalRoomModel(ctx, roomConfig)
	if err != nil {
		return err
	}
	info := sessionCommandInfo{
		Model:        canonicalModel,
		Reasoning:    displayReasoningLevel(cl.reasoningLevelForModel(model, roomConfig)),
		SystemPrompt: strings.TrimSpace(roomConfig.AdditionalPrompt) != "",
		Responding:   cl.getActiveRun(portal.PortalKey) != nil,
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

	branch, err := agentSession.GetBranch(ctx, nil)
	if err != nil {
		return err
	}
	info.Stats, err = sessionCommandStatsFromEntries(branch)
	if err != nil {
		return err
	}
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
	fmt.Fprintf(&text, "\n- Model: `%s`", info.Model)
	fmt.Fprintf(&text, "\n- Reasoning: `%s`", info.Reasoning)
	systemPrompt := "no"
	if info.SystemPrompt {
		systemPrompt = "yes"
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
	fmt.Fprintf(&text, "\n- Messages: `%d` total, `%d` user, `%d` assistant, `%d` tool results", stats.Messages, stats.UserMessages, stats.AssistantMessages, stats.ToolResultMessages)
	fmt.Fprintf(&text, "\n- Compactions: `%d`", stats.Compactions)
	fmt.Fprintf(&text, "\n- Branch entries: `%d`", stats.TotalEntries)
	return text.String()
}
