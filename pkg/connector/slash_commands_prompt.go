package connector

import (
	"context"
	"strings"

	"github.com/beeper/ai-bridge/pkg/aiid"
	"maunium.net/go/mautrix/bridgev2"
)

func runSystemPromptCommand(cl *Client, ctx context.Context, portal *bridgev2.Portal, roomConfig RoomConfig, arg string, responder aiCommandResponder) error {
	if strings.TrimSpace(arg) == "" {
		return responder.Reply(ctx, systemPromptStatusText(roomConfig))
	}
	prompt := arg
	if strings.EqualFold(prompt, "clear") || strings.EqualFold(prompt, "reset") {
		prompt = ""
	}
	if _, err := cl.writeRoomPromptState(ctx, portal, prompt); err != nil {
		return err
	}
	if prompt == "" {
		return responder.Reply(ctx, "System prompt cleared.")
	}
	return responder.Reply(ctx, "System prompt updated.")
}

func systemPromptStatusText(config RoomConfig) string {
	return currentSystemPromptText(config) + "\n\nOptions: `/system-prompt <prompt>`, `/system-prompt clear`."
}

func currentSystemPromptText(config RoomConfig) string {
	prompt := strings.TrimSpace(config.AdditionalPrompt)
	if prompt == "" {
		return "No additional system prompt is set."
	}
	return "Current system prompt:\n\n" + markdownCodeBlock(prompt)
}

func markdownCodeBlock(text string) string {
	fence := "```"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	return fence + "\n" + text + "\n" + fence
}

func (cl *Client) writeRoomPromptState(ctx context.Context, portal *bridgev2.Portal, prompt string) (string, error) {
	return cl.writeAIRoomState(ctx, portal, aiid.RoomPromptType, map[string]any{"prompt": strings.TrimSpace(prompt)})
}
