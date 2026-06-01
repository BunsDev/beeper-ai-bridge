package chattools

import (
	"context"
	"time"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func GetSessionTool(info SessionInfo) agent.AgentTool[any] {
	return GetSessionToolWithOptions(info, SessionOptions{})
}

func GetSessionToolWithOptions(info SessionInfo, options SessionOptions) agent.AgentTool[any] {
	return agent.AgentTool[any]{
		Tool: ai.Tool{
			Name:        "get_session",
			Description: "Get fresh metadata for this Beeper AI chat, including current UTC timestamp, chat ID/title, selected model, selected reasoning, disabled tools, approved profile fields, last message UTC timestamp, and last known timezone.",
			Parameters:  objectSchema(nil, nil),
		},
		Execute: func(ctx context.Context, toolCallID string, params any, onUpdate agent.AgentToolUpdateCallback[any]) (agent.AgentToolResult[any], error) {
			now := time.Now().UTC()
			current := info
			current.CurrentTimestamp = now.Format(time.RFC3339)
			if current.LastMessageTimestamp == "" {
				current.LastMessageTimestamp = current.CurrentTimestamp
			}
			if options.ResolveProfile != nil {
				profile, err := options.ResolveProfile(ctx, toolCallID)
				if err != nil {
					return agent.AgentToolResult[any]{}, err
				}
				applySessionProfile(&current, profile)
			}
			return jsonResult(current)
		},
	}
}

func applySessionProfile(info *SessionInfo, profile *SessionProfile) {
	if info == nil || profile == nil {
		return
	}
	info.BeeperUsername = profile.Username
	info.BeeperAccountEmail = profile.Email
	info.BeeperDisplayName = sessionProfileDisplayName(profile)
	info.GravatarProfile = profile.GravatarProfile
}

func sessionProfileDisplayName(profile *SessionProfile) string {
	if profile == nil {
		return ""
	}
	if profile.FullName != "" {
		return profile.FullName
	}
	switch matrixProfile := profile.MatrixProfile.(type) {
	case map[string]any:
		if displayName, _ := matrixProfile["displayname"].(string); displayName != "" {
			return displayName
		}
	case map[string]string:
		if displayName := matrixProfile["displayname"]; displayName != "" {
			return displayName
		}
	}
	return ""
}
