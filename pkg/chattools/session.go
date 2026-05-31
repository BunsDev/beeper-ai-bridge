package chattools

import (
	"context"
	"time"

	agent "github.com/beeper/ai-bridge/pkg/agent"
	ai "github.com/beeper/ai-bridge/pkg/ai"
)

func GetSessionTool(info SessionInfo) agent.AgentTool[any] {
	return agent.AgentTool[any]{
		Tool: ai.Tool{
			Name:        "get_session",
			Description: "Get fresh metadata for this Beeper AI chat, including current timestamp, timezone, room, session, model, reasoning, search, and attachments.",
			Parameters:  objectSchema(nil, nil),
		},
		Execute: func(ctx context.Context, toolCallID string, params any, onUpdate agent.AgentToolUpdateCallback[any]) (agent.AgentToolResult[any], error) {
			now := time.Now()
			current := info
			current.Timestamp = now.Format(time.RFC3339)
			current.Timezone = timezoneName(now)
			if current.ThreadID == "" {
				current.ThreadID = current.SessionID
			}
			return jsonResult(current)
		},
	}
}

func timezoneName(t time.Time) string {
	name, offset := t.Zone()
	if loc := t.Location().String(); loc != "" && loc != "Local" {
		return loc
	}
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	return name + " UTC" + sign + twoDigits(offset/3600) + ":" + twoDigits((offset%3600)/60)
}

func twoDigits(value int) string {
	if value < 10 {
		return "0" + string(rune('0'+value))
	}
	return string(rune('0'+value/10)) + string(rune('0'+value%10))
}
