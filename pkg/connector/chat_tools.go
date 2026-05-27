package connector

import (
	"fmt"
	"strings"
	"time"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	ai "github.com/beeper/ai-bridge/pkg/ai"
	"github.com/beeper/ai-bridge/pkg/aiid"
	"github.com/beeper/ai-bridge/pkg/chattools"
	"github.com/beeper/ai-bridge/pkg/msgconv"
	"maunium.net/go/mautrix/bridgev2"
)

func (cl *Client) chatTools(msg *bridgev2.MatrixMessage, meta *aiid.PortalMetadata, roomConfig RoomConfig, provider aiid.ProviderConfig, model ai.Model, prompt msgconv.MatrixPrompt) []agent.AgentTool[any] {
	roomID := ""
	roomTitle := ""
	if msg != nil && msg.Portal != nil {
		roomID = string(msg.Portal.MXID)
		if msg.Portal.NameSet {
			roomTitle = msg.Portal.Name
		}
	}
	info := chattools.SessionInfo{
		RoomTitle:       roomTitle,
		RoomID:          roomID,
		SessionID:       meta.SessionID,
		ThreadID:        meta.SessionID,
		LoginID:         string(cl.UserLogin.ID),
		ProviderID:      provider.ID,
		ModelID:         model.ID,
		ReasoningLevel:  cl.reasoningLevel(roomConfig),
		DisabledTools:   roomConfig.DisabledTools,
		AttachmentCount: len(prompt.Attachments),
	}
	for _, attachment := range prompt.Attachments {
		info.Attachments = append(info.Attachments, chattools.Attachment{Type: attachment.Type, MimeType: attachment.MimeType})
	}
	return chattools.Tools(info, chattools.FetchOptions{
		Timeout:  time.Duration(cl.Main.Config.Fetch.TimeoutMS) * time.Millisecond,
		MaxBytes: cl.Main.Config.Fetch.MaxBytes,
		MaxChars: cl.Main.Config.Fetch.MaxChars,
	}, chattools.SearchOptions{
		Enabled:  !toolDisabled(roomConfig.DisabledTools, "web_search") && cl.Main.Config.Search.Enabled,
		Endpoint: cl.Main.Config.Search.Endpoint,
		APIKey:   cl.Main.Config.Search.APIKey,
		Timeout:  10 * time.Second,
	})
}

func (cl *Client) systemPrompt(roomConfig RoomConfig) string {
	base := strings.TrimSpace(cl.Main.Config.DefaultSystemPrompt)
	room := strings.TrimSpace(roomConfig.AdditionalPrompt)
	switch {
	case base == "":
		return room
	case room == "":
		return base
	default:
		return base + "\n\n" + room
	}
}

func (cl *Client) reasoningLevel(roomConfig RoomConfig) string {
	if roomConfig.ThinkingLevel != "" {
		return roomConfig.ThinkingLevel
	}
	return cl.Main.Config.DefaultReasoningLevel
}

func (cl *Client) validateReasoningLevel(model ai.Model, roomConfig RoomConfig) error {
	level := ai.ModelThinkingLevel(cl.reasoningLevel(roomConfig))
	for _, supported := range ai.GetSupportedThinkingLevels(model) {
		if supported == level {
			return nil
		}
	}
	return fmt.Errorf("model %s does not support reasoning level %q", model.ID, level)
}

func toolDisabled(disabled []string, name string) bool {
	for _, disabledName := range disabled {
		if disabledName == name {
			return true
		}
	}
	return false
}
